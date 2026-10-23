package service

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// procParent reads pid's parent from /proc/<pid>/stat. The command name, the
// second field, is in parentheses and may hold spaces or parentheses itself,
// so the fields are read after its last ')' (proc(5): state, then ppid).
func procParent(pid int) (int, error) {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, err
	}
	s := string(raw)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return 0, fmt.Errorf("/proc/%d/stat: no command name", pid)
	}
	fields := strings.Fields(s[i+1:])
	if len(fields) < 2 {
		return 0, fmt.Errorf("/proc/%d/stat: too few fields", pid)
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, fmt.Errorf("/proc/%d/stat: parent %q: %w", pid, fields[1], err)
	}
	return ppid, nil
}
