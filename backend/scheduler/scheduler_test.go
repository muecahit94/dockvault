package scheduler

import "testing"

func TestValidateCron(t *testing.T) {
	for _, valid := range []string{"0 2 * * *", "*/15 * * * *", "@daily", "@every 6h"} {
		next, err := ValidateCron(valid)
		if err != nil {
			t.Fatalf("expected cron %q to be valid: %v", valid, err)
		}
		if next.IsZero() {
			t.Fatalf("expected non-zero next run for %q", valid)
		}
	}

	for _, invalid := range []string{"", "* * *", "not a cron", "99 99 * * *"} {
		if _, err := ValidateCron(invalid); err == nil {
			t.Fatalf("expected cron %q to fail validation", invalid)
		}
	}
}
