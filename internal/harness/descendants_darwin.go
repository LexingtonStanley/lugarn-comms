package harness

import "golang.org/x/sys/unix"

func processParents() (map[int]int, error) {
	processes, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, err
	}
	parents := make(map[int]int, len(processes))
	for _, process := range processes {
		parents[int(process.Proc.P_pid)] = int(process.Eproc.Ppid)
	}
	return parents, nil
}
