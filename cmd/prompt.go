package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"
)

// prompter reads operator input for interactive commands. It is a
// struct rather than a set of free functions so tests can drive a
// command with scripted input instead of a terminal.
type prompter struct {
	in  *bufio.Reader
	out io.Writer

	// interactive reports whether stdin is a terminal. When it is not
	// (a CI pipeline, a piped script), prompts fail loudly instead of
	// blocking forever or silently taking a default.
	interactive bool

	// stdinFd is the file descriptor used for masked password reads.
	// Zero-valued when input is not a terminal.
	stdinFd int
}

// errNotInteractive is returned when a prompt is needed but there is
// no terminal to ask.
var errNotInteractive = errors.New("no terminal available for interactive input")

// newPrompter builds a prompter over the process's stdin/stdout.
func newPrompter(out io.Writer) *prompter {
	fd := int(os.Stdin.Fd())
	return &prompter{
		in:          bufio.NewReader(os.Stdin),
		out:         out,
		interactive: term.IsTerminal(fd),
		stdinFd:     fd,
	}
}

// ask prints a question and returns the trimmed answer. An empty
// answer falls back to def.
func (p *prompter) ask(question, def string) (string, error) {
	if !p.interactive {
		return "", fmt.Errorf("%w: cannot prompt for %q", errNotInteractive, question)
	}

	if def != "" {
		fmt.Fprintf(p.out, "%s [%s]: ", question, def)
	} else {
		fmt.Fprintf(p.out, "%s: ", question)
	}

	line, err := p.in.ReadString('\n')
	if err != nil && (err != io.EOF || line == "") {
		return "", fmt.Errorf("cannot read input: %w", err)
	}

	answer := strings.TrimSpace(line)
	if answer == "" {
		return def, nil
	}
	return answer, nil
}

// askSecret reads a value without echoing it to the screen — used for
// access tokens and application secrets, which should not end up in
// terminal scrollback or a screen recording.
func (p *prompter) askSecret(question string) (string, error) {
	if !p.interactive {
		return "", fmt.Errorf("%w: cannot prompt for %q", errNotInteractive, question)
	}

	fmt.Fprintf(p.out, "%s: ", question)
	raw, err := term.ReadPassword(p.stdinFd)
	fmt.Fprintln(p.out)
	if err != nil {
		return "", fmt.Errorf("cannot read input: %w", err)
	}
	return strings.TrimSpace(string(raw)), nil
}

// choose presents a numbered list and returns the selected option.
// The operator can type either the number or the option text.
func (p *prompter) choose(question string, options []string, def string) (string, error) {
	if !p.interactive {
		return "", fmt.Errorf("%w: cannot prompt for %q", errNotInteractive, question)
	}

	fmt.Fprintf(p.out, "%s\n", question)
	for i, opt := range options {
		marker := " "
		if opt == def {
			marker = "*"
		}
		fmt.Fprintf(p.out, "  %s %d) %s\n", marker, i+1, opt)
	}

	for {
		answer, err := p.ask("Select", def)
		if err != nil {
			return "", err
		}

		if n, convErr := strconv.Atoi(answer); convErr == nil {
			if n >= 1 && n <= len(options) {
				return options[n-1], nil
			}
			fmt.Fprintf(p.out, "Enter a number between 1 and %d.\n", len(options))
			continue
		}

		for _, opt := range options {
			if strings.EqualFold(answer, opt) {
				return opt, nil
			}
		}
		fmt.Fprintf(p.out, "%q is not one of the options.\n", answer)
	}
}

// confirm asks a yes/no question. Mise's convention, matching the PRD,
// is that the safe answer is the default: "[y/N]" means no.
func (p *prompter) confirm(question string, def bool) (bool, error) {
	if !p.interactive {
		return false, fmt.Errorf("%w: cannot prompt for %q", errNotInteractive, question)
	}

	suffix := "[y/N]"
	if def {
		suffix = "[Y/n]"
	}

	for {
		fmt.Fprintf(p.out, "%s %s: ", question, suffix)

		line, err := p.in.ReadString('\n')
		if err != nil && (err != io.EOF || line == "") {
			return false, fmt.Errorf("cannot read input: %w", err)
		}

		switch strings.ToLower(strings.TrimSpace(line)) {
		case "":
			return def, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			fmt.Fprintln(p.out, "Please answer y or n.")
		}
	}
}
