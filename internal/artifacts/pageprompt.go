package artifacts

// The plain-language lines of the allow prompt for an HTML page (H6), shared by
// Studio and the CLI so both say the same things. They are fixed text: nothing
// of the page's own files or the person's data is interpolated except the name
// of the configured model provider.

// NetworkDisclosure is shown on macOS for a scripted page (H4, #189).
const NetworkDisclosure = "On macOS a scripted page can open a network connection to an address it chooses, including a computer on your network or this one, though it cannot read a reply. " +
	"Every other load, lookup and request is blocked, but this one gap exists until the page window can be isolated further."

// CredentialLine is shown for every page.
const CredentialLine = "Vibe never asks for a password, key or token inside a page. A page that asks for one is trying to steal it: deny it."

// HostLine says where a page of the derived kind runs and what it can reach.
func HostLine(kind string) string {
	if kind == PageScripted {
		return "This page has script, written by someone else or by the agent. It runs in its own window that cannot reach your files, your credentials or other pages, " +
			"and it can only run the queries listed here. It has no network it can use, except as noted below."
	}
	return "This page has no script. It shows inside Studio, in a frame that cannot reach the network, your files or other pages, " +
		"and it can only show what the queries listed here return."
}

// CapabilityLine is the plain line for one declared capability. askTarget is
// the usable model provider ("" when none is set up yet, in which case the line
// says so: the page's permission to ask stays on, so it will use whichever
// provider is set up later).
func CapabilityLine(name, askTarget string) string {
	switch name {
	case CapState:
		return "state: keeps small notes for this page on this computer only (up to 1 MB). Nothing is sent anywhere or saved in the workspace folder."
	case CapDownload:
		return "download: can offer a file it made (CSV, JSON, SVG, PNG, PDF or text, up to 20 MB). You always see a save dialog and choose where it goes, or cancel; " +
			"it only opens after you click or press a key in the page."
	case CapAsk:
		if askTarget == "" {
			return "ask: This page can send text, which may include query results, to whatever model provider is set up later (none is set now, so it gets no answer yet). You can switch this off separately in Settings > Workspace > Pages."
		}
		return "ask: This page can send text, which may include query results, to " + askTarget + ". You can switch this off separately in Settings > Workspace > Pages."
	}
	return name + ": unknown capability"
}
