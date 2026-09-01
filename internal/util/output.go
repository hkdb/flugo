package util

import (
	"fmt"
	"os"
)

// PrintBanner prints the Flugo ASCII art banner in green.
func PrintBanner() {
	fmt.Print("\n\033[32m")
	fmt.Println(`░▒▓████████▓▒░▒▓█▓▒░     ░▒▓█▓▒░░▒▓█▓▒░░▒▓██████▓▒░ ░▒▓██████▓▒░  `)
	fmt.Println(`░▒▓█▓▒░      ░▒▓█▓▒░     ░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░ `)
	fmt.Println(`░▒▓█▓▒░      ░▒▓█▓▒░     ░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░      ░▒▓█▓▒░░▒▓█▓▒░ `)
	fmt.Println(`░▒▓██████▓▒░ ░▒▓█▓▒░     ░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒▒▓███▓▒░▒▓█▓▒░░▒▓█▓▒░ `)
	fmt.Println(`░▒▓█▓▒░      ░▒▓█▓▒░     ░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░ `)
	fmt.Println(`░▒▓█▓▒░      ░▒▓████████▓▒░▒▓██████▓▒░ ░▒▓██████▓▒░ ░▒▓██████▓▒░  `)
	fmt.Print("\033[0m\n")
}

// PrintError prints an error message with "Error:" in red to stderr.
func PrintError(err error) {
	fmt.Fprintf(os.Stderr, "\033[1;31mError:\033[0m %s\n", err)
}

// PrintSuccess prints a green label followed by a message.
func PrintSuccess(label, message string) {
	fmt.Printf("\033[32m%s\033[0m %s\n", label, message)
}

// DoctorOK prints a green [OK] status line.
func DoctorOK(name, detail string) {
	fmt.Printf("  \033[32m[OK]\033[0m      %s: %s\n", name, detail)
}

// DoctorMissing prints a yellow [MISSING] status line.
func DoctorMissing(name, command string) {
	fmt.Printf("  \033[33m[MISSING]\033[0m %s (%s)\n", name, command)
}

// DoctorError prints a red [ERROR] status line.
func DoctorError(name string, err error) {
	fmt.Printf("  \033[31m[ERROR]\033[0m   %s: %v\n", name, err)
}
