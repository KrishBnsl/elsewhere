package main

import "testing"

func names(commands []paletteCommand) []string {
	var result []string
	for _, c := range commands {
		result = append(result, c.name)
	}
	return result
}

func TestMatchCommandsFuzzyAndSearchSafe(t *testing.T) {
	if got := names(matchCommands(slashCommands, "", false)); len(got) != len(slashCommands) {
		t.Fatalf("empty input should list everything, got %v", got)
	}
	if got := names(matchCommands(slashCommands, "thm", false)); len(got) != 1 || got[0] != "themes" {
		t.Fatalf("fuzzy thm = %v", got)
	}
	if got := names(matchCommands(slashCommands, "queue", false)); len(got) != 1 || got[0] != "q" {
		t.Fatalf("alias queue = %v", got)
	}
	if got := matchCommands(slashCommands, "dark side of the moon", false); got != nil {
		t.Fatalf("multi-word searches should not suggest commands: %v", got)
	}
	if got := names(matchCommands(colonCommands, "pick", true)); len(got) != 2 {
		t.Fatalf("colon prompts match descriptions too: %v", got)
	}
}

func TestSlashPaletteSelectionRunsCommand(t *testing.T) {
	t.Cleanup(func() { applyTheme(&themes[0]) })
	p, _, _ := queueFixture(t)
	p.handle("/")
	if p.paletteIndex != -1 || len(p.palette()) != len(slashCommands) {
		t.Fatalf("palette should list every command with nothing selected: %d", p.paletteIndex)
	}
	p.handle("t")
	p.handle("h")
	p.handle("\t")
	if p.selectedCommand() != "themes" {
		t.Fatalf("Tab should select the first match, got %q", p.selectedCommand())
	}
	p.handle("\r")
	if p.themePicker == nil || p.searching {
		t.Fatal("Enter on a selected command should open its screen")
	}
}

func TestSlashEnterWithoutSelectionStillSearches(t *testing.T) {
	p, _, _ := queueFixture(t)
	for _, key := range []string{"/", "r", "\r"} {
		p.handle(key)
	}
	if p.radioPicker != nil || p.query != "r" {
		t.Fatalf("typed text must stay a search unless a command is selected: query=%q picker=%v", p.query, p.radioPicker)
	}
}

func TestColonPaletteSelectsAndTypingClearsSelection(t *testing.T) {
	p, _, _ := queueFixture(t)
	p.handle(":")
	p.handle("\x1b[B")
	p.handle("\x1b[B")
	if p.selectedCommand() != "themes" {
		t.Fatalf("second item should be themes, got %q", p.selectedCommand())
	}
	p.handle("r")
	if p.selectedCommand() != "" {
		t.Fatal("typing should clear the highlighted selection")
	}
	p.handle("\x1b[B")
	if p.selectedCommand() != "radio" {
		t.Fatalf("got %q", p.selectedCommand())
	}
	p.handle("\r")
	if p.radioPicker == nil || p.command != nil {
		t.Fatal(":radio should open the station picker")
	}
}

func TestColonPaletteEnterOnQuitQuits(t *testing.T) {
	p, _, _ := queueFixture(t)
	p.handle(":")
	p.handle("\x1b[B")
	if p.handle("\r") {
		t.Fatal("selecting q in the colon palette should quit")
	}
}
