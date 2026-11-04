package config

import (
	"strings"
	"testing"
	"time"
)

func TestAuditRetentionEnv(t *testing.T) {
	for _, tc := range []struct {
		name    string
		set     bool
		val     string
		want    int
		wantErr bool
	}{
		{name: "unset defaults to 90", want: 90},
		{name: "empty defaults to 90", set: true, val: "", want: 90},
		{name: "blank defaults to 90", set: true, val: "  ", want: 90},
		{name: "explicit", set: true, val: "30", want: 30},
		{name: "padded", set: true, val: " 7 ", want: 7},
		{name: "zero is forever", set: true, val: "0", want: 0},
		{name: "max", set: true, val: "36500", want: 36500},
		{name: "negative", set: true, val: "-1", wantErr: true},
		{name: "non-numeric", set: true, val: "ninety", wantErr: true},
		{name: "fractional", set: true, val: "1.5", wantErr: true},
		{name: "too large", set: true, val: "36501", wantErr: true},
		{name: "overflow", set: true, val: "99999999999999999999", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearAuthEnv(t)
			if tc.set {
				t.Setenv("CONSOLE_AUDIT_RETENTION_DAYS", tc.val)
			} else {
				// Ensure it is genuinely unset for the test duration.
				t.Setenv("CONSOLE_AUDIT_RETENTION_DAYS", "")
			}
			c := FromEnv()
			err := c.Validate()
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "CONSOLE_AUDIT_RETENTION_DAYS") {
					t.Fatalf("want error naming the variable, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.AuditRetentionDays != tc.want {
				t.Fatalf("days = %d, want %d", c.AuditRetentionDays, tc.want)
			}
		})
	}
}

func TestAuditRetentionDefaultAndDuration(t *testing.T) {
	if d := Default().AuditRetentionDays; d != 90 {
		t.Fatalf("Default = %d, want 90", d)
	}
	c := Default()
	if c.AuditRetention() != 90*24*time.Hour {
		t.Fatalf("AuditRetention = %v", c.AuditRetention())
	}
	c.AuditRetentionDays = 0
	if c.AuditRetention() != 0 {
		t.Fatal("0 days must be a zero duration")
	}
	c.AuditRetentionDays = -1
	if c.Validate() == nil {
		t.Fatal("negative days set directly must fail Validate")
	}
	c.AuditRetentionDays = MaxAuditRetentionDays + 1
	if c.Validate() == nil {
		t.Fatal("over-max days set directly must fail Validate")
	}
}
