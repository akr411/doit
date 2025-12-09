# UI/UX Guidelines

**Purpose**: Detailed UI implementation specs for doit CLI
**Audience**: Developers working on UI code
**Last updated**: 2025-12-09

---

## Color Scheme

Defined in `internal/ui/styles.go`:

```go
Primary:  #5B8FF9  // Blue
Success:  #52C41A  // Green
Warning:  #FAAD14  // Yellow/Orange
Error:    #FF4D4F  // Red
Muted:    #8C8C8C  // Gray
```

## Helper Functions

```go
ui.PrintSuccess("✓ Completed %d todo(s)", count)  // Green
ui.PrintWarning("Warning: %v", err)               // Orange
ui.PrintError("Error: %v", err)                   // Red
```

---

## Display Rules

### General
- Use Lipgloss for all styling (maintain uniform look)
- Multi-line fields for long text (task/note inputs)
- Character limits: Task (200, warn at 150), Note (1000, warn at 800)
- Pagination: 10/25/50 items (configurable: `doit config pagination`)
- Retention: 10, 25, 50, 100, 200, 500 (configurable: `doit config retention`)
- No TTY vs non-TTY differences (identical output everywhere)

### Note Display
- Truncated to 50 characters (append "..." if longer)
- Indented below task with "│" prefix in muted color
- Multi-line notes collapsed to single line (newlines → spaces)
- Not shown in --json or --quiet modes
- Full note viewable: `doit note <id>`

### Deadline Display
- Show for pending tasks only (not completed)
- Position: After task name, e.g., "Task (due in 2h)"
- Formats:
  - "overdue" or "overdue Xd"
  - "due in Xh"
  - "due tomorrow"
  - "due in Xd"
  - "due 2025-12-31"
- Consistent across: CLI list, TUI interactive, interactive complete

### Warning Messages
- **CLI mode**: Print to stdout using `ui.PrintWarning()`
- **Interactive mode**: Show inline in orange, clears on next keypress
- **Examples**: Streak update failures, cleanup errors

---

## Interactive Behavior (Bubbletea TUI)

### General
- Inline (NOT fullscreen)
- Cursor: `>` (TUI standard)
- Checkboxes: `[ ]` pending, `[+]` selected, `[x]` completed

### Navigation
- Up/Down: `↑/↓` or `j/k`
- Left/Right: `←/→` or `h/l`
- Tab: `tab/shift+tab`

### Actions
- Selection: `x` key for multi-select (not space)
- Notes: `space` to show/hide (fullscreen if >10 lines, `q/esc` to close)
- Form submission: `ctrl+s` from any field, or `enter` in deadline field
- Form quit: `esc` quits silently (no error if not submitted)
- UI clearing: Only clear interactive UI on quit (not entire terminal)

### Pagination
- Pending tasks on first pages, completed tasks on last pages (separate)
- Edit/delete interactive: Filter out completed tasks (show pending only)
- Complete interactive: Show all tasks, allow toggle both ways

---

## Implementation Examples

### Styled Output
```go
// internal/ui/inline_list.go
style := lipgloss.NewStyle().
    Foreground(lipgloss.Color("#52C41A")).
    Bold(true)

fmt.Println(style.Render("✓ Task completed"))
```

### Truncation
```go
func truncateNote(note string, maxLen int) string {
    if len(note) <= maxLen {
        return note
    }
    return note[:maxLen] + "..."
}
```

### Deadline Formatting
```go
func formatDeadline(deadline int64) string {
    if deadline == 0 {
        return ""
    }

    now := time.Now().Unix()
    diff := deadline - now

    if diff < 0 {
        days := int(-diff / 86400)
        if days == 0 {
            return "overdue"
        }
        return fmt.Sprintf("overdue %dd", days)
    }

    hours := int(diff / 3600)
    if hours < 24 {
        return fmt.Sprintf("due in %dh", hours)
    }

    days := int(diff / 86400)
    if days == 1 {
        return "due tomorrow"
    }
    if days < 7 {
        return fmt.Sprintf("due in %dd", days)
    }

    return time.Unix(deadline, 0).Format("due 2006-01-02")
}
```

---

## Testing UI

### Manual Tests
```bash
# Test truncation
doit add -t "Task" -n "$(printf 'A%.0s' {1..100})"
doit list  # Note should be truncated to 50 chars

# Test deadline formats
doit add -t "Overdue" -d "-1h"
doit add -t "Soon" -d "2h"
doit add -t "Tomorrow" -d "25h"
doit list  # Check deadline displays

# Test pagination
for i in {1..30}; do doit add -t "Task $i"; done
doit list --page 1  # First 10
doit list --page 2  # Next 10
```

### Non-TTY Test
```bash
doit list | cat  # Should match TTY output
```
