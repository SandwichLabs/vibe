package llm

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/SandwichLabs/vibe/internal/agents"
)

// toolCategory looks up a tool's registered category (e.g. "data", "content",
// "schema"), or "" for an unknown tool name.
func toolCategory(name string) string {
	if td := agents.GetTool(name); td != nil {
		return td.Category
	}
	return ""
}

// Grounding check (#70), ported from schoolwork-workspace's
// src/agent/grounding.js. Chat-mode only — task mode is already forced
// through preview_query before any terminal write, and MCP has no
// free-text "final answer" to check.
//
// Incident this fixes: a summary asked for at a staff meeting was produced
// in 40 seconds with no query — "every student in our log is Tier 3" — 21
// of 30 actually were, a number the agent had itself reported correctly two
// questions earlier. Summarizing feels like recall rather than analysis, so
// a system-prompt rule alone did not stop it. This enforces it: if a turn
// ends with a factual claim and never touched the data, the claim is sent
// back once for verification before the user sees it settled.

// quantifierPatterns catch prose that asserts something countable ("every
// student", "none of them", "the majority of"). A summary of what the agent
// just DID ("I added a notebook cell") makes no such claim.
var quantifierPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(every|all|none|no)\s+(?:of\s+)?(?:the\s+|our\s+|your\s+)?\w*\s*(rows?|records?|tables?|customers?|orders?|users?|items?|entries?)\b`),
	regexp.MustCompile(`(?i)\b(?:no|none)\s+of\s+(?:them|the|our|your)\b`),
	regexp.MustCompile(`(?i)\bnobody\b|\bno one\b|\bnever\b|\balways\b`),
	regexp.MustCompile(`(?i)\b(?:the\s+)?(?:majority|most|few|fewest|many)\s+of\b`),
	regexp.MustCompile(`(?i)\b(?:highest|lowest|worst|best|top|bottom)\s+\w*\s*(rows?|records?|scores?|values?)\b`),
}

// bareNumberPattern flags a bare number in prose as a claim about the data.
// Excludes numbers immediately preceded by a word char/./$ or followed by a
// word char/%, so ordinary things like "2026", "50%", or a code identifier
// don't trip it on their own (those still fall through to the fenced-code
// strip below when they appear inside SQL/backticks).
var bareNumberPattern = regexp.MustCompile(`(?:^|[^\w.$])\d{1,6}(?:[.,]\d+)?(?:$|[^\w%])`)

var fencedCodeRe = regexp.MustCompile("(?s)```.*?```")
var inlineCodeRe = regexp.MustCompile("`[^`]*`")

// assertsFact reports whether text asserts a factual quantity about the
// data (a count, a superlative, a "every/none/always" claim) after
// stripping fenced code blocks and inline SQL, which otherwise let a
// query's own literals read as claims about the data.
//
// Deliberately generous: a false positive costs one verification turn, a
// false negative puts an unchecked number in front of someone about to
// repeat it.
func assertsFact(text string) bool {
	t := strings.TrimSpace(text)
	if len(t) < 12 {
		return false
	}
	prose := fencedCodeRe.ReplaceAllString(text, " ")
	prose = inlineCodeRe.ReplaceAllString(prose, " ")

	for _, re := range quantifierPatterns {
		if re.MatchString(prose) {
			return true
		}
	}
	return bareNumberPattern.MatchString(prose)
}

// groundingTools are the tool calls that count as "looked at the data this
// turn" — run_query and preview_query (both execute real SQL against the
// container), describe_table (only when it actually returned sample rows —
// today's describe_table never does, so this key is future-proofing, not
// dead: see the #70 report), and present_data when called with a live "sql"
// binding rather than inline data.
var groundingTools = map[string]bool{
	"run_query":     true,
	"preview_query": true,
}

// usedGroundingTool reports whether any recorded tool step this turn counts
// as grounding — see groundingTools. present_data and describe_table need
// their Input inspected (only a live sql-bound call/sample grounds), so
// they're handled here rather than by name alone.
func usedGroundingTool(steps []ToolStep) bool {
	for _, s := range steps {
		if s.IsError {
			continue
		}
		if groundingTools[s.Tool] {
			return true
		}
		if s.Tool == "present_data" && toolInputHasNonEmptyString(s.Input, "sql") {
			return true
		}
		if s.Tool == "describe_table" && toolOutputHasSample(s.Output) {
			return true
		}
	}
	return false
}

func toolInputHasNonEmptyString(input, key string) bool {
	var params map[string]any
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return false
	}
	v, ok := params[key].(string)
	return ok && strings.TrimSpace(v) != ""
}

func toolOutputHasSample(output string) bool {
	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return false
	}
	_, ok := result["sample"]
	return ok
}

// writeToolCategories are the agents.ToolDef categories that always perform
// a write when the tool succeeds (mirrors internal/agents' unexported
// isWriteOperation, minus "data" — see wroteThisTurn for why "data" needs
// its own case rather than a blanket category match).
var writeToolCategories = map[string]bool{
	"content": true, "schema": true,
}

// wroteThisTurn reports whether any successful tool call this turn actually
// wrote something (review fix: grounding must never send a turn that just
// wrote data back through another tool-calling pass — even with the nudge's
// "do not repeat any action" instruction, the safest fix is to not give the
// model another chance to re-invoke a write tool at all).
//
// run_query is deliberately excluded from writeToolCategories's blanket
// "data" category match: by the time this is called, usedGroundingTool has
// already returned false, which — since it matches "run_query" by name
// alone, regardless of what SQL it ran — proves no run_query call succeeded
// this turn at all (a successful run_query, read or write, always makes
// usedGroundingTool true and short-circuits maybeGroundText before
// wroteThisTurn is ever reached). So run_query needs no separate check
// here; every other "data"-category tool (create_record/update_record/
// delete_record/bulk_insert) is unconditionally a write.
func wroteThisTurn(steps []ToolStep) bool {
	for _, s := range steps {
		if s.IsError {
			continue
		}
		if s.Tool == "run_query" || s.Tool == "preview_query" {
			continue
		}
		cat := toolCategory(s.Tool)
		if cat == "data" || writeToolCategories[cat] {
			return true
		}
	}
	return false
}

// groundingPrompt is the nudge sent back for a single re-answer when
// needsGrounding fires. Names the specific failure rather than scolding
// generically: the model is not unwilling to check, it did not notice that
// recalling and knowing had come apart.
const groundingPrompt = `Stop. You just answered with figures or a claim about the data without running a query this turn — you answered from memory of earlier turns.

Re-answer the same question, but this time:
1. Run run_query (or preview_query) for every factual claim you intend to make, including ones you believe you already know. Numbers from an earlier turn may have been about a different filter, table, or subset.
2. If a query contradicts what you just said, say so plainly and give the corrected figure.
3. If something cannot be verified with the data loaded, say that instead of asserting it.
4. Do not repeat any action; only verify with read-only queries.

Give the full re-answer, not a diff.`
