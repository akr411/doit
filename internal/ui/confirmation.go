package ui

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func Confirm(message string) (bool, error) {
	fmt.Printf("%s (y/n): ", message)

	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return false, fmt.Errorf("failed to read input")
	}

	response := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return response == "y" || response == "yes", nil
}
