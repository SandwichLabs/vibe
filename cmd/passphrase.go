package cmd

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

// readPasswordFunc is the function used to read passphrases from the terminal.
// Override in tests to avoid requiring a real terminal.
var readPasswordFunc = func() func(fd int) ([]byte, error) {
	return term.ReadPassword
}()

// promptPassphrase reads a passphrase from the terminal with echo disabled.
func promptPassphrase(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	password, err := readPasswordFunc(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr) // newline after input
	if err != nil {
		return "", fmt.Errorf("failed to read passphrase: %w", err)
	}
	return string(password), nil
}

// promptPassphraseConfirm prompts for a passphrase twice and checks they match.
func promptPassphraseConfirm() (string, error) {
	pass1, err := promptPassphrase("Enter passphrase: ")
	if err != nil {
		return "", err
	}
	if pass1 == "" {
		return "", fmt.Errorf("passphrase cannot be empty")
	}

	pass2, err := promptPassphrase("Confirm passphrase: ")
	if err != nil {
		return "", err
	}

	if pass1 != pass2 {
		return "", fmt.Errorf("passphrases do not match")
	}

	return pass1, nil
}
