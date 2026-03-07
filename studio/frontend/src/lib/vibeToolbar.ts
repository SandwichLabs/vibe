import type { EditorView } from '@codemirror/view'

export interface VibeToolbarButton {
  id: string
  icon: string
  tooltip: string
  snippet: string
}

/** Insert a snippet at the current cursor position (or replace selection). */
export function insertSnippet(view: EditorView, snippet: string): void {
  const { from, to } = view.state.selection.main
  view.dispatch({
    changes: { from, to, insert: snippet },
    selection: { anchor: from + snippet.length },
  })
  view.focus()
}

/** SVG icon helper — wraps path in a 20x20 SVG. */
function svg(path: string): string {
  return `<svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="square" stroke-linejoin="miter">${path}</svg>`
}

export const vibeToolbarButtons: VibeToolbarButton[] = [
  {
    id: 'sql-block',
    icon: svg('<path d="M4 7h16M4 12h16M4 17h10"/><circle cx="19" cy="17" r="2"/>'),
    tooltip: 'Insert SQL Block',
    snippet: '```sql name=query_name\nSELECT * FROM table_name\n```\n',
  },
  {
    id: 'bar-chart',
    icon: svg('<rect x="3" y="12" width="4" height="9"/><rect x="10" y="6" width="4" height="15"/><rect x="17" y="9" width="4" height="12"/>'),
    tooltip: 'Insert Bar Chart',
    snippet: '{bar_chart: source=query_name, x=column1, y=column2}\n',
  },
  {
    id: 'line-chart',
    icon: svg('<polyline points="3 18 9 12 13 15 21 6"/><polyline points="17 6 21 6 21 10"/>'),
    tooltip: 'Insert Line Chart',
    snippet: '{line_chart: source=query_name, x=column1, y=column2}\n',
  },
  {
    id: 'pie-chart',
    icon: svg('<path d="M12 2a10 10 0 1 0 10 10H12V2z"/><path d="M12 2a10 10 0 0 1 10 10H12V2z" fill="currentColor" opacity="0.3"/>'),
    tooltip: 'Insert Pie Chart',
    snippet: '{pie_chart: source=query_name, labels=column1, values=column2}\n',
  },
  {
    id: 'table',
    icon: svg('<rect x="3" y="3" width="18" height="18"/><line x1="3" y1="9" x2="21" y2="9"/><line x1="3" y1="15" x2="21" y2="15"/><line x1="9" y1="3" x2="9" y2="21"/><line x1="15" y1="3" x2="15" y2="21"/>'),
    tooltip: 'Insert Table',
    snippet: '{table: source=query_name}\n',
  },
  {
    id: 'metric',
    icon: svg('<path d="M12 20V4"/><path d="M6 12h12"/><circle cx="12" cy="12" r="9"/>'),
    tooltip: 'Insert Metric',
    snippet: '{metric: source=query_name, value=column_name, title="Metric Title"}\n',
  },
]
