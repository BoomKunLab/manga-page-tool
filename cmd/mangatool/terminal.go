package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

var errPromptCancelled = errors.New("選択を取り消しました")

type promptKind int

const (
	promptDirectory promptKind = iota
	promptChoice
	promptInput
	promptConfirm
)

type promptOption struct {
	label          string
	value          string
	navigationOnly bool
}

type terminalPrompt struct {
	kind          promptKind
	title         string
	directory     string
	input         textinput.Model
	options       []promptOption
	matches       []int
	selected      int
	width, height int
	status        string
	validate      func(string) error
	result        string
	accepted      bool
}

func newPrompt(kind promptKind, title string) *terminalPrompt {
	input := textinput.New()
	input.Prompt = "> "
	input.SetWidth(74)
	// Use terminal paste events without invoking a system clipboard command.
	input.KeyMap.Paste.SetEnabled(false)
	return &terminalPrompt{kind: kind, title: title, input: input, width: 80, height: 24}
}

func (m *terminalPrompt) run() (string, error) {
	if !term.IsTerminal(os.Stdin.Fd()) || !term.IsTerminal(os.Stderr.Fd()) {
		return "", errors.New("対話操作には端末が必要です。コマンドの引数を指定してください")
	}
	m.filter()
	_, err := tea.NewProgram(m, tea.WithInput(os.Stdin), tea.WithOutput(os.Stderr)).Run()
	if errors.Is(err, tea.ErrInterrupted) {
		return "", errPromptCancelled
	}
	if err != nil {
		return "", err
	}
	if !m.accepted {
		return "", errPromptCancelled
	}
	return m.result, nil
}

func (m *terminalPrompt) Init() tea.Cmd {
	if m.kind == promptDirectory || m.kind == promptInput {
		return m.input.Focus()
	}
	return nil
}

func (m *terminalPrompt) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.SetWidth(max(1, msg.Width-6))
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "ctrl+c":
			return m, tea.Quit
		case "enter":
			return m, m.enter()
		case "up":
			m.selected = max(0, m.selected-1)
			return m, nil
		case "down":
			m.selected = min(m.selected+1, max(0, len(m.matches)-1))
			return m, nil
		case "tab":
			if m.kind == promptDirectory && len(m.matches) > 0 {
				m.openDirectory(m.options[m.matches[m.selected]].value)
				return m, nil
			}
		}
	}
	if m.kind != promptDirectory && m.kind != promptInput {
		return m, nil
	}
	old := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != old {
		m.status = ""
		if m.kind == promptDirectory {
			m.filter()
		}
	}
	return m, cmd
}

func (m *terminalPrompt) enter() tea.Cmd {
	switch m.kind {
	case promptDirectory, promptChoice:
		if len(m.matches) == 0 {
			return nil
		}
		option := m.options[m.matches[m.selected]]
		if option.navigationOnly {
			m.status = "確定するにはフォルダ名の行を選んで Enter を押してください"
			return nil
		}
		m.result = option.value
	case promptInput:
		value := m.input.Value()
		if err := m.validate(value); err != nil {
			m.status = err.Error()
			return nil
		}
		m.result = value
	case promptConfirm:
		// Confirm the entire plan, regardless of the visible row.
	}
	m.accepted = true
	return tea.Quit
}

func (m *terminalPrompt) filter() {
	query := strings.ToLower(strings.TrimSpace(m.input.Value()))
	m.matches = m.matches[:0]
	for i, option := range m.options {
		if fuzzyMatch(strings.ToLower(option.label), query) {
			m.matches = append(m.matches, i)
		}
	}
	m.selected = 0
}

func fuzzyMatch(value, query string) bool {
	for _, candidate := range value {
		if query == "" {
			break
		}
		r, size := utf8.DecodeRuneInString(query)
		if candidate == r {
			query = query[size:]
		}
	}
	return query == ""
}

func (m *terminalPrompt) View() tea.View {
	if m.accepted {
		return tea.NewView("")
	}
	lines := []string{displayText(m.title)}
	switch m.kind {
	case promptDirectory:
		lines = append(lines, displayText(m.directory), "絞り込み "+m.input.View())
	case promptInput:
		lines = append(lines, m.input.View())
	}
	if m.kind != promptInput {
		rows := max(1, m.height-len(lines)-4)
		first := max(0, m.selected-rows+1)
		for i := first; i < min(first+rows, len(m.matches)); i++ {
			marker := "  "
			if i == m.selected {
				marker = "› "
			}
			lines = append(lines, marker+displayText(m.options[m.matches[i]].label))
		}
		if len(m.matches) == 0 {
			lines = append(lines, "一致する候補がありません")
		} else if len(m.matches) > rows {
			lines = append(lines, fmt.Sprintf("%d–%d / %d", first+1, min(first+rows, len(m.matches)), len(m.matches)))
		}
	}
	if m.status != "" {
		lines = append(lines, displayText(m.status))
	}
	switch m.kind {
	case promptDirectory:
		lines = append(lines, "↑↓ 移動  Tab フォルダを開く  Enter フォルダ名を確定  Esc 取消")
	case promptChoice:
		lines = append(lines, "↑↓ 移動  Enter 選択  Esc 取消")
	case promptInput:
		lines = append(lines, "Enter 次へ  Esc 取消")
	case promptConfirm:
		lines = append(lines, "↑↓ スクロール  Enter リネーム実行  Esc 取消")
	}
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, max(1, m.width), "…")
	}
	view := tea.NewView(strings.Join(lines, "\n"))
	view.AltScreen = true
	return view
}

// Avoid interpreting terminal controls embedded in a filename or path.
func displayText(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return '�'
		}
		return r
	}, value)
}

func (m *terminalPrompt) openDirectory(path string) {
	entries, err := os.ReadDir(path)
	if err != nil {
		m.status = err.Error()
		return
	}
	m.directory = path
	m.status = ""
	m.input.SetValue("")
	m.options = nil
	if parent := filepath.Dir(path); parent != path {
		m.options = append(m.options, promptOption{label: "..  親フォルダ（Tab で移動）", value: parent, navigationOnly: true})
	}
	for _, entry := range entries {
		full := filepath.Join(path, entry.Name())
		isDir := entry.IsDir()
		if entry.Type()&os.ModeSymlink != 0 {
			info, err := os.Stat(full)
			isDir = err == nil && info.IsDir()
		}
		if isDir {
			m.options = append(m.options, promptOption{label: entry.Name(), value: full})
		}
	}
	m.filter()
}

func selectDirectory() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	prompt := newPrompt(promptDirectory, "入力フォルダを選択")
	prompt.openDirectory(dir)
	if prompt.status != "" {
		return "", errors.New(prompt.status)
	}
	return prompt.run()
}

func readPrompt(title, initial string, validate func(string) error) (string, error) {
	prompt := newPrompt(promptInput, title)
	prompt.input.Placeholder = initial
	prompt.validate = validate
	return prompt.run()
}
