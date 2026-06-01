//go:build windows

package core

import "testing"

func TestParseNetstat(t *testing.T) {
	out := "  Proto  Local Address    Foreign Address   State        PID\n" +
		"  TCP    0.0.0.0:3000     0.0.0.0:0         LISTENING    1234\n" +
		"  TCP    [::]:4983        [::]:0            LISTENING    1234\n" +
		"  TCP    0.0.0.0:9000     0.0.0.0:0         LISTENING    5\n" + // out of range
		"  TCP    0.0.0.0:3000     1.2.3.4:55        ESTABLISHED  77\n" // not listening
	ls := parseNetstat(out, PortRange{Lo: 3000, Hi: 5000})
	if len(ls) != 2 {
		t.Fatalf("got %d: %+v", len(ls), ls)
	}
}

func TestParseTasklist(t *testing.T) {
	out := "\"node.exe\",\"1234\",\"Console\",\"1\",\"50,000 K\"\n" +
		"\"bun.exe\",\"5678\",\"Console\",\"1\",\"60,000 K\"\n"
	m := parseTasklist(out)
	if m[1234] != "node.exe" || m[5678] != "bun.exe" {
		t.Errorf("got %v", m)
	}
}
