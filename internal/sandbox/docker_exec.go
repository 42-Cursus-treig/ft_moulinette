package sandbox

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

// syncBuffer protège le buffer par mutex : en cas de timeout on lit la sortie
// pendant qu'une goroutine d'os/exec peut encore y écrire - un bytes.Buffer nu
// serait une data race.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// runDocker exécute `docker <args...>` (args commence par "run") avec un nom de
// conteneur unique et un timeout. SEUL point d'entrée vers `docker run` du
// package.
func runDocker(args []string, timeout time.Duration, stdin io.Reader) (stdout, stderr string, timedOut bool, err error) {
	name := "ftm-" + randomContainerSuffix()

	fullArgs := make([]string, 0, len(args)+2)
	fullArgs = append(fullArgs, args[0]) // "run"
	fullArgs = append(fullArgs, "--name", name)
	fullArgs = append(fullArgs, args[1:]...)

	var outBuf, errBuf syncBuffer
	cmd := exec.Command("docker", fullArgs...)
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	if stdin != nil {
		cmd.Stdin = stdin
	}

	if err := cmd.Start(); err != nil {
		return "", "", false, fmt.Errorf("lancement de docker impossible: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case runErr := <-done:
		killContainer(name)
		return outBuf.String(), errBuf.String(), false, runErr

	case <-time.After(timeout):
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		killContainer(name)
		go func() { <-done }()
		return outBuf.String(), errBuf.String(), true, fmt.Errorf("timeout dépassé (%s)", timeout)
	}
}

// killContainer tue le conteneur par son nom. Best-effort : échoue
// silencieusement s'il s'est déjà auto-supprimé (--rm).
func killContainer(name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	exec.CommandContext(ctx, "docker", "kill", name).Run() //nolint:errcheck
}

func randomContainerSuffix() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		// Un nom non-unique ferait juste échouer le --name suivant, pas planter.
		return "fallback"
	}
	return hex.EncodeToString(b)
}
