package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/Lumos-Labs-HQ/kmax/internal/cmd"
	"github.com/Lumos-Labs-HQ/kmax/internal/ui"
)

func printHelp() {
	fmt.Println()
	fmt.Println(ui.Bold(ui.Cyan("kmax")) + ui.Dim(" — Kiro session manager"))
	fmt.Println()
	fmt.Println(ui.Bold("COMMANDS"))
	rows := [][2]string{
		{"  list          ", "List all sessions with status"},
		{"  swap          ", "Auto-swap to next available session"},
		{"  use <id>      ", "Force swap to a specific session"},
		{"  end <id>      ", "Mark a session as ended"},
		{"  reset [<id>]  ", "Unend all sessions (or one), clearing used_at"},
		{"  credits [<id>]", "Show live credit usage (defaults to active)"},
		{"  login         ", "Log in to a new account and save it as a session"},
		{"  sync -f cac   ", "Sync conversations from CachyOS → Ubuntu"},
		{"  sync -f ubu   ", "Sync conversations from Ubuntu → CachyOS"},
		{"  continue, c   ", "Pick & resume a previous conversation"},
	}
	for _, r := range rows {
		fmt.Println(ui.Cyan(r[0]) + ui.Dim(r[1]))
	}
	fmt.Println()
}

func launchKiro(args ...string) {
	bin, err := exec.LookPath("kiro-cli")
	if err != nil {
		fmt.Fprintln(os.Stderr, "kiro-cli not found on PATH")
		os.Exit(1)
	}
	syscall.Exec(bin, append([]string{"kiro-cli"}, args...), os.Environ())
}

func main() {
	if len(os.Args) < 2 {
		launchKiro()
		return
	}

	switch os.Args[1] {
	case "list":
		cmd.List()
	case "swap":
		fmt.Println()
		cmd.Swap()
		fmt.Println()
	case "use":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: kmax use <id>")
			os.Exit(1)
		}
		fmt.Println()
		cmd.Use(os.Args[2])
		fmt.Println()
	case "end":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: kmax end <id|name>")
			os.Exit(1)
		}
		fmt.Println()
		cmd.End(os.Args[2])
		fmt.Println()
	case "reset":
		arg := ""
		if len(os.Args) >= 3 {
			arg = os.Args[2]
		}
		fmt.Println()
		cmd.Reset(arg)
		fmt.Println()
	case "credits":
		arg := ""
		if len(os.Args) >= 3 {
			arg = os.Args[2]
		}
		cmd.Credits(arg)
	case "sync":
		from := ""
		for i, a := range os.Args[2:] {
			if a == "-f" && i+1 < len(os.Args[2:]) {
				from = os.Args[i+3]
			}
		}
		if from == "" {
			fmt.Fprintln(os.Stderr, "usage: kmax sync -f cac|ubu")
			os.Exit(1)
		}
		fmt.Println()
		cmd.Sync(from)
	case "login":
		fmt.Println()
		cmd.Login()
		fmt.Println()
	case "continue", "c":
		launchKiro("chat", "--resume-picker")
	case "help", "-h", "--help":
		printHelp()
	default:
		launchKiro(os.Args[1:]...)
	}
}
