package store

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

const (
	concurrencyDBEnv     = "LUGARN_STORE_TEST_CONCURRENCY_DB"
	concurrencyWorkerEnv = "LUGARN_STORE_TEST_CONCURRENCY_WORKER"
)

func TestConcurrentWriterProcess(t *testing.T) {
	path := os.Getenv(concurrencyDBEnv)
	if path == "" {
		t.Skip("child-process helper")
	}
	worker, err := strconv.Atoi(os.Getenv(concurrencyWorkerEnv))
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	}()
	if err := incrementAndInsert(s, worker, 200); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentWritersAcrossProcessesAndGoroutines(t *testing.T) {
	if os.Getenv(concurrencyDBEnv) != "" {
		t.Skip("parent-only test")
	}
	path := filepath.Join(t.TempDir(), "concurrent.db")
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	}()
	writeTest(t, s, func(tx *Tx) error {
		if err := tx.CreateTeam(testTeam()); err != nil {
			return err
		}
		if err := tx.InsertMember(humanParams()); err != nil {
			return err
		}
		return tx.PutHopCounter(HopCounter{TeamID: "team", Allowance: 2000})
	})

	const processCount = 4
	commands := make([]*exec.Cmd, processCount)
	outputs := make([]bytes.Buffer, processCount)
	for worker := 0; worker < processCount; worker++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestConcurrentWriterProcess$", "-test.count=1")
		cmd.Env = append(os.Environ(), concurrencyDBEnv+"="+path, concurrencyWorkerEnv+"="+strconv.Itoa(worker+1))
		cmd.Stdout = &outputs[worker]
		cmd.Stderr = &outputs[worker]
		commands[worker] = cmd
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
	}
	for worker, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("writer %d: %v\n%s", worker+1, err, outputs[worker].String())
		}
	}

	const goroutineCount = 4
	const goroutineWrites = 50
	start := make(chan struct{})
	errorsCh := make(chan error, goroutineCount)
	var wait sync.WaitGroup
	for worker := 0; worker < goroutineCount; worker++ {
		worker := worker + processCount + 1
		wait.Add(1)
		go func() {
			defer wait.Done()
			store, err := Open(context.Background(), path)
			if err != nil {
				errorsCh <- err
				return
			}
			defer store.Close()
			<-start
			if err := incrementAndInsert(store, worker, goroutineWrites); err != nil {
				errorsCh <- err
			}
		}()
	}
	close(start)
	wait.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}

	readTest(t, s, func(tx *Tx) error {
		hops, err := tx.GetHopCounter("team")
		if err != nil {
			return err
		}
		expected := int64(processCount*200 + goroutineCount*goroutineWrites)
		if hops.Used != expected {
			t.Fatalf("hop count = %d, want %d", hops.Used, expected)
		}
		rows, err := tx.query("SELECT seq FROM messages ORDER BY seq")
		if err != nil {
			return err
		}
		defer rows.Close()
		var seq int64
		var expectedSeq int64
		for rows.Next() {
			if err := rows.Scan(&seq); err != nil {
				return err
			}
			expectedSeq++
			if seq != expectedSeq {
				t.Fatalf("message seq = %d at position %d", seq, expectedSeq)
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if expectedSeq != expected {
			t.Fatalf("messages = %d, want %d", expectedSeq, expected)
		}
		return nil
	})
}

func incrementAndInsert(s *Store, worker, count int) error {
	for i := 0; i < count; i++ {
		messageID := id(worker*100000 + i + 100)
		err := s.Write(context.Background(), func(tx *Tx) error {
			counter, err := tx.GetHopCounter("team")
			if err != nil {
				return err
			}
			counter.Used++
			if err := tx.PutHopCounter(*counter); err != nil {
				return err
			}
			_, err = tx.InsertMessage(MessageParams{SenderID: id(1), Run: 1, Message: modelMessage(messageID)})
			return err
		})
		if err != nil {
			return fmt.Errorf("worker %d write %d: %w", worker, i, err)
		}
	}
	return nil
}

func modelMessage(messageID string) model.Message {
	return model.Message{ID: messageID, Team: "team", Kind: model.MessageKindMessage, Text: "concurrent"}
}
