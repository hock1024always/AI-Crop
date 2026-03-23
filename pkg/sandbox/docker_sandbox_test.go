package sandbox

import (
	"context"
	"testing"
	"time"
)

// ============================================
// Docker Sandbox Unit Tests
// ============================================

func TestDefaultSandboxConfig(t *testing.T) {
	config := DefaultSandboxConfig()

	if config.MemoryMB != 512 {
		t.Errorf("Expected MemoryMB 512, got %d", config.MemoryMB)
	}
	if config.CPUQuota != 50000 {
		t.Errorf("Expected CPUQuota 50000, got %d", config.CPUQuota)
	}
	if config.NetworkEnabled {
		t.Error("Expected NetworkEnabled to be false by default")
	}
	if config.ExecutionTimeout != 5*time.Minute {
		t.Errorf("Expected ExecutionTimeout 5m, got %v", config.ExecutionTimeout)
	}
}

func TestCodeExecutionSandbox(t *testing.T) {
	config := CodeExecutionSandbox()

	if config.MemoryMB != 1024 {
		t.Errorf("Expected MemoryMB 1024, got %d", config.MemoryMB)
	}
	if config.NetworkEnabled {
		t.Error("Code execution sandbox should not have network by default")
	}
	if config.ExecutionTimeout != 10*time.Minute {
		t.Errorf("Expected ExecutionTimeout 10m, got %v", config.ExecutionTimeout)
	}
}

func TestWebScraperSandbox(t *testing.T) {
	config := WebScraperSandbox()

	if !config.NetworkEnabled {
		t.Error("Web scraper sandbox should have network enabled")
	}
	if len(config.AllowedHosts) == 0 {
		t.Error("Web scraper should have allowed hosts whitelist")
	}
}

func TestDataProcessingSandbox(t *testing.T) {
	config := DataProcessingSandbox()

	if config.MemoryMB != 2048 {
		t.Errorf("Expected MemoryMB 2048, got %d", config.MemoryMB)
	}
	if config.CPUCount != 2.0 {
		t.Errorf("Expected CPUCount 2.0, got %f", config.CPUCount)
	}
}

func TestBuildDockerRunArgs(t *testing.T) {
	sm, err := NewSandboxManager(nil)
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}

	sandbox := &TaskSandbox{
		ID:      "test-sandbox-123",
		TaskID:  "task-456",
		Image:   "python:3.11-slim",
		Config:  DefaultSandboxConfig(),
	}

	args := sm.buildDockerRunArgs(sandbox, "/tmp/test")

	// Verify essential arguments
	assertContains(t, args, "--memory")
	assertContains(t, args, "--cpu-quota")
	assertContains(t, args, "--no-new-privileges")
	assertContains(t, args, "--network")
	assertContains(t, args, "--cap-drop=ALL")
	assertContains(t, args, "python:3.11-slim")
}

func assertContains(t *testing.T, args []string, expected string) {
	for _, arg := range args {
		if arg == expected || (len(arg) > len(expected) && arg[:len(expected)] == expected) {
			return
		}
	}
	t.Errorf("Expected args to contain %s, got %v", expected, args)
}

// ============================================
// Integration Tests (require Docker)
// ============================================

func TestSandboxLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	sm, err := NewSandboxManager(nil)
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	defer sm.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Create sandbox
	sandbox, err := sm.CreateSandbox(ctx, "test-task-001", "python:3.11-slim", &SandboxConfig{
		MemoryMB:        256,
		CPUQuota:        25000,
		CPUPeriod:       100000,
		CPUCount:        0.5,
		ExecutionTimeout: 30 * time.Second,
		NetworkEnabled:   false,
		WorkDir:          "/tmp/ai-corp-sandbox-test",
	})
	if err != nil {
		t.Fatalf("Failed to create sandbox: %v", err)
	}

	if sandbox.Status != "running" {
		t.Errorf("Expected status 'running', got '%s'", sandbox.Status)
	}

	// Execute command
	output, exitCode, err := sm.ExecuteInSandbox(ctx, sandbox.ID, []string{"python3", "-c", "print('Hello from sandbox')"})
	if err != nil && exitCode != 0 {
		t.Logf("Execution warning: %v", err)
	}
	t.Logf("Output: %s, ExitCode: %d", output, exitCode)

	// Stop sandbox
	if err := sm.StopSandbox(sandbox.ID); err != nil {
		t.Logf("Cleanup warning: %v", err)
	}
}

func TestSandboxResourceLimits(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	sm, err := NewSandboxManager(nil)
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	defer sm.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	// Create sandbox with strict limits
	sandbox, err := sm.CreateSandbox(ctx, "test-memory-limit", "python:3.11-slim", &SandboxConfig{
		MemoryMB:        128, // Very limited memory
		CPUQuota:        10000,
		CPUPeriod:       100000,
		CPUCount:        0.1,
		ExecutionTimeout: 30 * time.Second,
		NetworkEnabled:   false,
		WorkDir:          "/tmp/ai-corp-sandbox-test",
	})
	if err != nil {
		t.Fatalf("Failed to create sandbox: %v", err)
	}

	// Try to allocate more memory than allowed (should fail or be limited)
	output, _, _ := sm.ExecuteInSandbox(ctx, sandbox.ID, []string{
		"python3", "-c",
		`import sys; x = " " * (200 * 1024 * 1024); print("Allocated 200MB")`,
	})
	t.Logf("Memory test output: %s", output)

	sm.StopSandbox(sandbox.ID)
}

func TestSandboxTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	sm, err := NewSandboxManager(nil)
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	defer sm.Close()

	// Create sandbox with short timeout
	ctx := context.Background()
	sandbox, err := sm.CreateSandbox(ctx, "test-timeout", "python:3.11-slim", &SandboxConfig{
		MemoryMB:         256,
		CPUQuota:         25000,
		CPUPeriod:        100000,
		CPUCount:         0.5,
		ExecutionTimeout: 2 * time.Second, // Very short timeout
		NetworkEnabled:   false,
		WorkDir:          "/tmp/ai-corp-sandbox-test",
	})
	if err != nil {
		t.Fatalf("Failed to create sandbox: %v", err)
	}

	// Execute long-running command
	execCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	_, _, err = sm.ExecuteInSandbox(execCtx, sandbox.ID, []string{
		"python3", "-c", "import time; time.sleep(10); print('Done')",
	})

	if err == nil {
		t.Error("Expected timeout error")
	} else {
		t.Logf("Got expected error: %v", err)
	}

	sm.StopSandbox(sandbox.ID)
}

func TestSandboxNetworkIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	sm, err := NewSandboxManager(nil)
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	defer sm.Close()

	ctx := context.Background()

	// Create sandbox without network
	sandbox, err := sm.CreateSandbox(ctx, "test-no-network", "python:3.11-slim", &SandboxConfig{
		MemoryMB:         256,
		CPUQuota:         25000,
		CPUPeriod:        100000,
		CPUCount:         0.5,
		ExecutionTimeout: 30 * time.Second,
		NetworkEnabled:   false,
		WorkDir:          "/tmp/ai-corp-sandbox-test",
	})
	if err != nil {
		t.Fatalf("Failed to create sandbox: %v", err)
	}

	// Try to access network (should fail)
	output, exitCode, _ := sm.ExecuteInSandbox(ctx, sandbox.ID, []string{
		"python3", "-c",
		`import urllib.request; urllib.request.urlopen("http://example.com")`,
	})

	if exitCode == 0 {
		t.Error("Network should be disabled")
	}
	t.Logf("Network test output: %s", output)

	sm.StopSandbox(sandbox.ID)
}

// ============================================
// Benchmark Tests
// ============================================

func BenchmarkSandboxCreation(b *testing.B) {
	sm, err := NewSandboxManager(nil)
	if err != nil {
		b.Skipf("Docker not available: %v", err)
	}
	defer sm.Close()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sandbox, err := sm.CreateSandbox(ctx, "bench-task", "python:3.11-slim", nil)
		if err != nil {
			b.Fatalf("Failed to create sandbox: %v", err)
		}
		sm.StopSandbox(sandbox.ID)
	}
}
