package engine

// Column describes a single column in a query result.
type Column struct {
	Name string
	Type string // go type name
}

// ResultSet holds the results of a query execution.
type ResultSet struct {
	Columns []Column
	Rows    [][]any
}

// RowCount returns the number of rows in the result set.
func (rs *ResultSet) RowCount() int {
	return len(rs.Rows)
}

// Value returns the value at the given row and column index.
// Returns nil if the indices are out of bounds.
func (rs *ResultSet) Value(row, col int) any {
	if row < 0 || row >= len(rs.Rows) {
		return nil
	}
	if col < 0 || col >= len(rs.Rows[row]) {
		return nil
	}
	return rs.Rows[row][col]
}

// ColumnIndex returns the index of the column with the given name,
// or -1 if no such column exists.
func (rs *ResultSet) ColumnIndex(name string) int {
	for i, c := range rs.Columns {
		if c.Name == name {
			return i
		}
	}
	return -1
}
