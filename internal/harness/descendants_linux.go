package harness

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Read numeric process ancestry only. No executable names or global signalling.
func processParents() (map[int]int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	parents := map[int]int{}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		stat, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if err != nil {
			continue
		}
		end := strings.LastIndex(string(stat), ") ")
		if end < 0 {
			continue
		}
		fields := strings.Fields(string(stat)[end+2:])
		if len(fields) < 2 {
			continue
		}
		parent, err := strconv.Atoi(fields[1])
		if err == nil {
			parents[pid] = parent
		}
	}
	return parents, nil
}
