package utils

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

func ParseDeadline(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}

	durationRegex := regexp.MustCompile(`^(\d+)([hdwmy])$`)
	if matches := durationRegex.FindStringSubmatch(s); matches != nil {
		value, _ := strconv.Atoi(matches[1])
		unit := matches[2]

		var duration time.Duration
		switch unit {
		case "h":
			duration = time.Hour * time.Duration(value)
		case "d":
			duration = time.Hour * 24 * time.Duration(value)
		case "w":
			duration = time.Hour * 24 * 7 * time.Duration(value)
		case "m":
			duration = time.Hour * 24 * 30 * time.Duration(value)
		case "y":
			duration = time.Hour * 24 * 365 * time.Duration(value)
		}

		return time.Now().Add(duration).Unix(), nil
	}

	t, err := time.Parse("2006-01-02", s)
	if err == nil {
		return t.Unix(), nil
	}

	t, err = time.Parse("2006-01-02 15:04", s)
	if err == nil {
		return t.Unix(), nil
	}

	return 0, fmt.Errorf("invalid deadline format: use '2h', '1d', or '2006-01-02'")
}

func FormatDeadline(deadline int64) string {
	if deadline == 0 {
		return ""
	}

	now := time.Now()
	t := time.Unix(deadline, 0)

	if t.Before(now) {
		diff := now.Sub(t)
		days := int(diff.Hours() / 24)
		if days == 0 {
			return "overdue"
		}
		return fmt.Sprintf("overdue %dd", days)
	}

	diff := t.Sub(now)
	hours := int(diff.Hours())

	if hours < 24 {
		return fmt.Sprintf("due in %dh", hours)
	}

	days := hours / 24
	if days == 1 {
		return "due tomorrow"
	}
	if days <= 2 {
		return fmt.Sprintf("due in %dd", days)
	}

	return fmt.Sprintf("due %s", t.Format("2006-01-02"))
}
