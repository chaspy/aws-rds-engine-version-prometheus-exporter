package main

import (
	"testing"
	"time"
)

type testdata struct {
	name      string
	validDate string
	now       time.Time
	out       string
}

func TestValidateEOLDate(t *testing.T) {
	t.Parallel()

	var layout = "2006-01-02"
	nowForTest := "2021-01-01"
	now, err := time.Parse(layout, nowForTest)
	if err != nil {
		t.Fatalf("failed to parse time: %#v", err)
	}

	tests := []testdata{
		{name: "Expired", validDate: "2020-12-31", now: now, out: "expired"},
		{name: "Alert", validDate: "2021-01-01", now: now, out: "alert"},
		{name: "Alert", validDate: "2021-01-02", now: now, out: "alert"},
		{name: "Warning", validDate: "2021-03-30", now: now, out: "alert"},
		{name: "Warning", validDate: "2021-04-01", now: now, out: "warning"},
		{name: "Warning", validDate: "2021-06-29", now: now, out: "warning"},
		{name: "OK", validDate: "2021-06-30", now: now, out: "ok"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := validateEOLDate(tt.validDate, tt.now)
			if err != nil {
				t.Fatalf("failed to calid EOL date: %#v", err)
			}
			if result != tt.out {
				t.Fatalf("result wants %v but got %v when input validDate %v now %v", tt.out, result, tt.validDate, tt.now)
			}
		})
	}
}
