package secrets

import "runtime"

// BackendName names the OS keychain Vibe talks to, for the settings screen.
func BackendName() string {
	switch runtime.GOOS {
	case "darwin":
		return "macOS Keychain"
	case "windows":
		return "Windows Credential Manager"
	default:
		return "Secret Service (GNOME Keyring or KDE Wallet)"
	}
}
