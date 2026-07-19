package ptr

import "time"

func Int(i int) *int {
	return &i
}

func String(s string) *string {
	return &s
}

func Duration(d time.Duration) *time.Duration {
	return &d
}
