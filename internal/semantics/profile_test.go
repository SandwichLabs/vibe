package semantics

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/slaw/internal/engine"
)

const profileOrders = "version: 1\nname: orders\ngrain: one row per order\ntable: prod_pg.public.orders\nconnection: prod_pg\nid_column: order_id\n"
const profileCustomer = "version: 1\nname: customer\ngrain: one row per customer\ntable: customers\nconnection: crm\nid_column: customer_id\n"

// profileJoinFile has a comment, a blank line and a stale measured block in the middle.
const profileJoinFile = `# keep this comment
version: 1
name: orders_customers
left: orders
right: customer
on:
  - {left: customer_id, right: customer_id}
  - {left: region, right: region}
cardinality: many_to_one
measured:
  at: 2026-01-01T00:00:00Z
  left_rows: 1
  right_rows: 1
  max_fanout: 1
  left_orphans: 0
  right_orphans: 0
  verdict: consistent

description: trailing key after the block
`

func profileStore(t *testing.T, joinFile string) *Mock {
	t.Helper()
	m := &Mock{}
	m.SetFile("entities/orders.yaml", []byte(profileOrders))
	m.SetFile("entities/customer.yaml", []byte(strings.Replace(profileCustomer, "connection: crm", "connection: prod_pg", 1)))
	m.SetFile("joins/orders_customers.yaml", []byte(joinFile))
	return m
}

// answers maps a statement's role (found by a marker in its text) to a count.
func answers(left, right, lo, ro, fan, dup int64, seen *[]string) QueryFunc {
	return func(_ context.Context, conn, sql string, params map[string]any) (*engine.ResultSet, error) {
		if seen != nil {
			*seen = append(*seen, conn+"|"+sql)
		}
		n := int64(0)
		switch {
		case sql == `SELECT count(*) FROM "prod_pg"."public"."orders"`:
			n = left
		case sql == `SELECT count(*) FROM "customers"`:
			n = right
		case strings.Contains(sql, "WHERE NOT EXISTS") && strings.Contains(sql, `FROM "prod_pg"."public"."orders" AS slaw_l WHERE NOT`):
			n = lo
		case strings.Contains(sql, "WHERE NOT EXISTS"):
			n = ro
		case strings.Contains(sql, "AND EXISTS"):
			n = fan
		default:
			n = dup
		}
		return &engine.ResultSet{Rows: [][]any{{n}}}, nil
	}
}

func TestPlanProfile_Shapes(t *testing.T) {
	j, err := DecodeJoin("orders_customers", []byte(profileJoinFile))
	require.NoError(t, err)
	l, err := DecodeEntity("orders", []byte(profileOrders))
	require.NoError(t, err)
	r, err := DecodeEntity("customer", []byte(strings.Replace(profileCustomer, "crm", "prod_pg", 1)))
	require.NoError(t, err)
	p, err := PlanProfile(j, l, r)
	require.NoError(t, err)
	assert.Equal(t, `SELECT count(*) FROM "prod_pg"."public"."orders"`, p.LeftRows.SQL)
	assert.Equal(t, `SELECT count(*) FROM "customers"`, p.RightRows.SQL)
	assert.Equal(t, `SELECT count(*) FROM "prod_pg"."public"."orders" AS slaw_l WHERE NOT EXISTS (SELECT 1 FROM "customers" AS slaw_r WHERE slaw_r."customer_id" = slaw_l."customer_id" AND slaw_r."region" = slaw_l."region")`, p.LeftOrphans.SQL)
	assert.Equal(t, `SELECT count(*) FROM "customers" AS slaw_r WHERE NOT EXISTS (SELECT 1 FROM "prod_pg"."public"."orders" AS slaw_l WHERE slaw_r."customer_id" = slaw_l."customer_id" AND slaw_r."region" = slaw_l."region")`, p.RightOrphans.SQL)
	assert.Equal(t, `SELECT coalesce(max(c), 0) FROM (SELECT count(*) AS c FROM "customers" AS slaw_r WHERE slaw_r."customer_id" IS NOT NULL AND slaw_r."region" IS NOT NULL AND EXISTS (SELECT 1 FROM "prod_pg"."public"."orders" AS slaw_l WHERE slaw_r."customer_id" = slaw_l."customer_id" AND slaw_r."region" = slaw_l."region") GROUP BY slaw_r."customer_id", slaw_r."region") AS slaw_g`, p.MaxFanout.SQL)
	assert.Equal(t, "prod_pg", p.LeftOrphans.Connection)

	// Two unqualified tables on different connections cannot share a statement.
	r2, _ := DecodeEntity("customer", []byte(profileCustomer))
	_, err = PlanProfile(j, l2(t, "table: orders", l), r2)
	assert.ErrorContains(t, err, "different connections")
	// A catalog-qualified left table needs no connection of its own.
	p, err = PlanProfile(j, l, r2)
	require.NoError(t, err)
	assert.Equal(t, "crm", p.LeftOrphans.Connection)
}

func l2(t *testing.T, tableLine string, l *Entity) *Entity {
	t.Helper()
	c := *l
	c.Table = strings.TrimPrefix(tableLine, "table: ")
	return &c
}

func TestProfileJoin_WritesMeasuredKeepingTheRestOfTheFile(t *testing.T) {
	st := profileStore(t, profileJoinFile)
	var seen []string
	now := time.Date(2026, 10, 29, 15, 30, 12, 0, time.UTC)
	p, err := ProfileJoin(context.Background(), st, "orders_customers", answers(12000, 800, 12, 3, 1, 1, &seen), now)
	require.NoError(t, err)
	assert.Equal(t, VerdictOrphans, p.Measured.Verdict, "12 left rows have no customer in a many_to_one")
	assert.Len(t, seen, 6)
	assert.NotEmpty(t, p.Rev)

	src, err := st.Source(context.Background(), KindJoin, "orders_customers")
	require.NoError(t, err)
	assert.Equal(t, p.Rev, src.Rev)
	assert.True(t, strings.HasPrefix(src.Text, "# keep this comment\nversion: 1\n"))
	assert.Contains(t, src.Text, "measured:\n  at: 2026-10-29T15:30:12Z\n  left_rows: 12000\n  right_rows: 800\n  max_fanout: 1\n  left_orphans: 12\n  right_orphans: 3\n  verdict: orphans\n\ndescription: trailing key after the block\n")
	assert.Equal(t, 1, strings.Count(src.Text, "measured:"))
	j, err := st.Join(context.Background(), "orders_customers")
	require.NoError(t, err)
	assert.Equal(t, Count(12), j.Measured.LeftOrphans)
	assert.Equal(t, "trailing key after the block", j.Description)
	_, warnings := judge(j.Cardinality, j.Measured, 0)
	assert.Equal(t, []string{"12 left rows have no match on the right (an inner join drops them)"}, warnings, "right orphans are normal in many_to_one")
}

func TestProfileJoin_AppendsWhenThereIsNoBlockAndJudgesFanOut(t *testing.T) {
	file := strings.Replace(profileJoinFile, profileJoinFile[strings.Index(profileJoinFile, "measured:"):strings.Index(profileJoinFile, "description:")], "", 1)
	st := profileStore(t, strings.TrimSuffix(file, "\n"))
	p, err := ProfileJoin(context.Background(), st, "orders_customers", answers(10, 5, 0, 2, 3, 1, nil), time.Now())
	require.NoError(t, err)
	assert.Equal(t, VerdictFanOut, p.Measured.Verdict)
	require.NotEmpty(t, p.Warnings)
	assert.Contains(t, p.Warnings[0], "declared many_to_one, but a left key matches up to 3 right rows")
	src, _ := st.Source(context.Background(), KindJoin, "orders_customers")
	assert.True(t, strings.HasSuffix(src.Text, "verdict: fan_out\n"), src.Text)
}

func TestJudge(t *testing.T) {
	cases := []struct {
		card    string
		m       Measured
		leftDup Count
		want    string
	}{
		{ManyToOne, Measured{MaxFanout: 1, RightOrphans: 50}, 1, VerdictConsistent},
		{ManyToOne, Measured{MaxFanout: 2}, 1, VerdictFanOut},
		{ManyToOne, Measured{MaxFanout: 2, LeftOrphans: 1}, 1, VerdictFanOut},
		{OneToMany, Measured{MaxFanout: 9, LeftOrphans: 4}, 1, VerdictConsistent},
		{OneToMany, Measured{RightOrphans: 1}, 1, VerdictOrphans},
		{OneToMany, Measured{}, 2, VerdictFanOut},
		{OneToOne, Measured{MaxFanout: 1, RightOrphans: 1}, 1, VerdictOrphans},
		{OneToOne, Measured{MaxFanout: 2}, 1, VerdictFanOut},
		{ManyToMany, Measured{MaxFanout: 99, LeftOrphans: 9, RightOrphans: 9}, 99, VerdictConsistent},
	}
	for _, c := range cases {
		got, _ := judge(c.card, &c.m, c.leftDup)
		assert.Equal(t, c.want, got, "%s %+v dup %d", c.card, c.m, c.leftDup)
	}
}

// The file changing while the profile runs is a conflict; nothing is written.
func TestProfileJoin_StaleFileIsAConflict(t *testing.T) {
	st := profileStore(t, profileJoinFile)
	edited := strings.Replace(profileJoinFile, "# keep this comment", "# edited by someone else", 1)
	calls := 0
	run := func(ctx context.Context, conn, sql string, params map[string]any) (*engine.ResultSet, error) {
		calls++
		if calls == 3 {
			st.SetFile("joins/orders_customers.yaml", []byte(edited))
		}
		return &engine.ResultSet{Rows: [][]any{{int64(1)}}}, nil
	}
	_, err := ProfileJoin(context.Background(), st, "orders_customers", run, time.Now())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrConflict)
	src, _ := st.Source(context.Background(), KindJoin, "orders_customers")
	assert.Equal(t, edited, src.Text, "the other writer's file is untouched")
}

func TestProfileJoin_Refusals(t *testing.T) {
	ctx := context.Background()
	_, err := ProfileJoin(ctx, profileStore(t, profileJoinFile), "nope", nil, time.Now())
	assert.ErrorIs(t, err, ErrNotFound)

	st := profileStore(t, "version: 1\nname: orders_customers\nleft: orders\n")
	_, err = ProfileJoin(ctx, st, "orders_customers", nil, time.Now())
	assert.ErrorContains(t, err, "cannot be profiled")

	st = profileStore(t, profileJoinFile)
	st.SetFile("entities/customer.yaml", []byte("garbage: ["))
	_, err = ProfileJoin(ctx, st, "orders_customers", nil, time.Now())
	assert.ErrorContains(t, err, "right entity")

	st = profileStore(t, profileJoinFile)
	boom := func(context.Context, string, string, map[string]any) (*engine.ResultSet, error) {
		return nil, errors.New("connection prod_pg is down")
	}
	_, err = ProfileJoin(ctx, st, "orders_customers", boom, time.Now())
	assert.ErrorContains(t, err, "prod_pg is down")
	src, _ := st.Source(ctx, KindJoin, "orders_customers")
	assert.Equal(t, profileJoinFile, src.Text, "a failed profile writes nothing")
}

func TestWithMeasured_FlowStyleBlockIsReplaced(t *testing.T) {
	text := "version: 1\nname: orders_customers\nleft: orders\nright: customer\non: [{left: a, right: b}]\ncardinality: one_to_one\nmeasured: {at: 2026-01-01T00:00:00Z, left_rows: 1, right_rows: 1, max_fanout: 1, left_orphans: 0, right_orphans: 0, verdict: consistent}\n"
	j, err := DecodeJoin("orders_customers", []byte(text))
	require.NoError(t, err)
	m := Measured{At: "2026-10-29T00:00:00Z", LeftRows: 5, RightRows: 5, MaxFanout: 1, Verdict: VerdictConsistent}
	out, err := withMeasured(j, text, m)
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(out, "measured:"))
	assert.NotContains(t, out, "{at:")
}

func TestProfileJoin_CRLFFileKeepsItsLineEndings(t *testing.T) {
	crlf := strings.ReplaceAll(profileJoinFile, "\n", "\r\n")
	st := profileStore(t, crlf)
	_, err := ProfileJoin(context.Background(), st, "orders_customers", answers(5, 5, 0, 0, 1, 1, nil), time.Now())
	require.NoError(t, err)
	src, _ := st.Source(context.Background(), KindJoin, "orders_customers")
	assert.NotRegexp(t, `[^\r]\n`, src.Text, "every line keeps CRLF")
	assert.Contains(t, src.Text, "measured:\r\n  at: ")
}

// withMeasured refuses a file it cannot edit before any scan runs.
func TestProfileJoin_UneditableFileFailsBeforeTheScans(t *testing.T) {
	flow := "version: 1\nname: orders_customers\nleft: orders\nright: customer\non: [{left: customer_id, right: customer_id}]\ncardinality: many_to_one\nmeasured: {at: 2026-01-01T00:00:00Z, left_rows: 1, right_rows: 1, max_fanout: 1, left_orphans: 0, right_orphans: 0, verdict: consistent}\nx: 1\n"
	st := profileStore(t, flow)
	calls := 0
	run := func(context.Context, string, string, map[string]any) (*engine.ResultSet, error) {
		calls++
		return &engine.ResultSet{Rows: [][]any{{int64(1)}}}, nil
	}
	_, err := ProfileJoin(context.Background(), st, "orders_customers", run, time.Now())
	require.Error(t, err)
	assert.Zero(t, calls)
}

func TestProfileJoin_CrossConnectionIsRefusedBeforeAnyQuery(t *testing.T) {
	st := profileStore(t, profileJoinFile)
	st.SetFile("entities/customer.yaml", []byte(profileCustomer)) // connection: crm, table unqualified; orders is on prod_pg
	st.SetFile("entities/orders.yaml", []byte(strings.Replace(profileOrders, "prod_pg.public.orders", "orders", 1)))
	calls := 0
	run := func(context.Context, string, string, map[string]any) (*engine.ResultSet, error) {
		calls++
		return nil, nil
	}
	_, err := ProfileJoin(context.Background(), st, "orders_customers", run, time.Now())
	require.Error(t, err)
	assert.ErrorContains(t, err, "different connections")
	assert.Zero(t, calls)
}
