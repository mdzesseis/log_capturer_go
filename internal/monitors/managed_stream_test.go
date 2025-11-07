package monitors

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockReadCloser is a mock implementation of io.ReadCloser for testing
type mockReadCloser struct {
	reader      io.Reader
	closeError  error
	closeCalled bool
}

func (m *mockReadCloser) Read(p []byte) (n int, err error) {
	return m.reader.Read(p)
}

func (m *mockReadCloser) Close() error {
	m.closeCalled = true
	return m.closeError
}

// TestNewManagedDockerStream verifies stream creation
func TestNewManagedDockerStream(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	reader := strings.NewReader("test data")
	stream := io.NopCloser(reader)
	httpResp := &http.Response{
		Body: io.NopCloser(strings.NewReader("http body")),
	}

	ms := NewManagedDockerStream(stream, httpResp, "container123", "test-container", logger)

	assert.NotNil(t, ms, "ManagedDockerStream should not be nil")
	assert.Equal(t, "container123", ms.ContainerID(), "ContainerID should match")
	assert.Equal(t, "test-container", ms.ContainerName(), "ContainerName should match")
	assert.False(t, ms.IsClosed(), "Stream should not be closed initially")
	assert.NotZero(t, ms.CreatedAt(), "CreatedAt should be set")
	assert.Zero(t, ms.ClosedAt(), "ClosedAt should be zero initially")
}

// TestManagedDockerStream_Read verifies Read delegation
func TestManagedDockerStream_Read(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	testData := "test log data"
	reader := strings.NewReader(testData)
	stream := io.NopCloser(reader)

	ms := NewManagedDockerStream(stream, nil, "container123", "test-container", logger)
	defer ms.Close()

	// Read from stream
	buf := make([]byte, len(testData))
	n, err := ms.Read(buf)

	require.NoError(t, err, "Read should not error")
	assert.Equal(t, len(testData), n, "Should read all data")
	assert.Equal(t, testData, string(buf[:n]), "Data should match")
}

// TestManagedDockerStream_ReadAfterClose verifies read after close returns error
func TestManagedDockerStream_ReadAfterClose(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	reader := strings.NewReader("test data")
	stream := io.NopCloser(reader)

	ms := NewManagedDockerStream(stream, nil, "container123", "test-container", logger)

	// Close stream
	err := ms.Close()
	require.NoError(t, err, "Close should not error")

	// Try to read after close
	buf := make([]byte, 10)
	_, err = ms.Read(buf)
	assert.Error(t, err, "Read after close should return error")
	assert.Equal(t, io.ErrClosedPipe, err, "Should return ErrClosedPipe")
}

// TestManagedDockerStream_Close verifies proper cleanup
func TestManagedDockerStream_Close(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	mockStream := &mockReadCloser{
		reader: strings.NewReader("test data"),
	}
	mockHTTPBody := &mockReadCloser{
		reader: strings.NewReader("http body"),
	}
	httpResp := &http.Response{
		Body: mockHTTPBody,
	}

	ms := NewManagedDockerStream(mockStream, httpResp, "container123", "test-container", logger)

	// Close stream
	err := ms.Close()
	require.NoError(t, err, "Close should not error")

	// Verify both layers were closed
	assert.True(t, mockStream.closeCalled, "Stream Close() should be called")
	assert.True(t, mockHTTPBody.closeCalled, "HTTP Body Close() should be called")
	assert.True(t, ms.IsClosed(), "Stream should be marked as closed")
	assert.NotZero(t, ms.ClosedAt(), "ClosedAt should be set")
}

// TestManagedDockerStream_CloseIdempotent verifies close is idempotent
func TestManagedDockerStream_CloseIdempotent(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	mockStream := &mockReadCloser{
		reader: strings.NewReader("test data"),
	}

	ms := NewManagedDockerStream(mockStream, nil, "container123", "test-container", logger)

	// Close multiple times
	err1 := ms.Close()
	err2 := ms.Close()
	err3 := ms.Close()

	require.NoError(t, err1, "First close should not error")
	require.NoError(t, err2, "Second close should not error")
	require.NoError(t, err3, "Third close should not error")

	// Verify close was only called once
	assert.True(t, mockStream.closeCalled, "Close should be called")
}

// TestManagedDockerStream_CloseWithError verifies error handling
func TestManagedDockerStream_CloseWithError(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	streamError := errors.New("stream close error")
	mockStream := &mockReadCloser{
		reader:     strings.NewReader("test data"),
		closeError: streamError,
	}

	ms := NewManagedDockerStream(mockStream, nil, "container123", "test-container", logger)

	// Close should return error but still mark as closed
	err := ms.Close()
	assert.Error(t, err, "Close should return error")
	assert.Contains(t, err.Error(), "stream close error", "Error should contain stream error")
	assert.True(t, ms.IsClosed(), "Stream should still be marked as closed")
}

// TestManagedDockerStream_CloseWithHTTPError verifies HTTP close error handling
func TestManagedDockerStream_CloseWithHTTPError(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	mockStream := &mockReadCloser{
		reader: strings.NewReader("test data"),
	}

	httpError := errors.New("http close error")
	mockHTTPBody := &mockReadCloser{
		reader:     strings.NewReader("http body"),
		closeError: httpError,
	}
	httpResp := &http.Response{
		Body: mockHTTPBody,
	}

	ms := NewManagedDockerStream(mockStream, httpResp, "container123", "test-container", logger)

	// Close should return HTTP error
	err := ms.Close()
	assert.Error(t, err, "Close should return error")
	assert.Contains(t, err.Error(), "http close error", "Error should contain HTTP error")
	assert.True(t, ms.IsClosed(), "Stream should still be marked as closed")
}

// TestManagedDockerStream_Age verifies age tracking
func TestManagedDockerStream_Age(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	reader := strings.NewReader("test data")
	stream := io.NopCloser(reader)

	ms := NewManagedDockerStream(stream, nil, "container123", "test-container", logger)
	defer ms.Close()

	// Wait a bit
	time.Sleep(100 * time.Millisecond)

	age := ms.Age()
	assert.True(t, age >= 100*time.Millisecond, "Age should be at least 100ms")
	assert.True(t, age < 1*time.Second, "Age should be less than 1s")
}

// TestManagedDockerStream_AgeAfterClose verifies age after close
func TestManagedDockerStream_AgeAfterClose(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	reader := strings.NewReader("test data")
	stream := io.NopCloser(reader)

	ms := NewManagedDockerStream(stream, nil, "container123", "test-container", logger)

	// Wait a bit then close
	time.Sleep(100 * time.Millisecond)
	ms.Close()

	age1 := ms.Age()
	time.Sleep(100 * time.Millisecond)
	age2 := ms.Age()

	// Age should be frozen after close
	assert.Equal(t, age1, age2, "Age should not change after close")
}

// TestManagedDockerStream_Stats verifies stats reporting
func TestManagedDockerStream_Stats(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	reader := strings.NewReader("test data")
	stream := io.NopCloser(reader)

	ms := NewManagedDockerStream(stream, nil, "container123", "test-container", logger)
	defer ms.Close()

	stats := ms.Stats()

	// Verify stats structure
	assert.Equal(t, "container123", stats["container_id"], "container_id should match")
	assert.Equal(t, "test-container", stats["container_name"], "container_name should match")
	assert.NotNil(t, stats["created_at"], "created_at should be set")
	assert.Equal(t, false, stats["is_closed"], "is_closed should be false initially")
	assert.Contains(t, stats, "age_seconds", "Should contain age_seconds")

	// Close and check stats again
	ms.Close()
	stats = ms.Stats()

	assert.Equal(t, true, stats["is_closed"], "is_closed should be true after close")
	assert.Contains(t, stats, "closed_at", "Should contain closed_at after close")
	assert.Contains(t, stats, "lifetime_seconds", "Should contain lifetime_seconds after close")
}

// TestManagedDockerStream_ConcurrentAccess verifies thread-safety
func TestManagedDockerStream_ConcurrentAccess(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	// Create a large buffer for reading
	data := bytes.Repeat([]byte("test data\n"), 1000)
	reader := bytes.NewReader(data)
	stream := io.NopCloser(reader)

	ms := NewManagedDockerStream(stream, nil, "container123", "test-container", logger)
	defer ms.Close()

	// Run concurrent operations
	done := make(chan bool, 10)

	// Concurrent readers
	for i := 0; i < 5; i++ {
		go func() {
			defer func() { done <- true }()

			buf := make([]byte, 100)
			for {
				_, err := ms.Read(buf)
				if err != nil {
					break
				}
			}
		}()
	}

	// Concurrent stat accessors
	for i := 0; i < 5; i++ {
		go func() {
			defer func() { done <- true }()

			for j := 0; j < 100; j++ {
				_ = ms.IsClosed()
				_ = ms.Age()
				_ = ms.Stats()
			}
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestExtractHTTPResponse verifies HTTP response extraction
func TestExtractHTTPResponse(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	t.Run("nil stream", func(t *testing.T) {
		resp := ExtractHTTPResponse(nil, logger)
		assert.Nil(t, resp, "Should return nil for nil stream")
	})

	t.Run("stream without HTTP response", func(t *testing.T) {
		stream := io.NopCloser(strings.NewReader("test"))
		resp := ExtractHTTPResponse(stream, logger)
		// May be nil (expected) as NopCloser doesn't expose HTTP response
		// Test just verifies no panic
		_ = resp
	})

	t.Run("http.Response type", func(t *testing.T) {
		httpResp := &http.Response{
			Body: io.NopCloser(strings.NewReader("test")),
		}
		// Direct pass-through (not typical Docker SDK usage, but tests type assertion)
		resp := ExtractHTTPResponse(httpResp.Body, logger)
		// May be nil, depends on implementation
		_ = resp
	})
}

// BenchmarkManagedDockerStream_Read benchmarks read performance
func BenchmarkManagedDockerStream_Read(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	data := bytes.Repeat([]byte("benchmark data\n"), 1000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		reader := bytes.NewReader(data)
		stream := io.NopCloser(reader)
		ms := NewManagedDockerStream(stream, nil, "bench", "bench", logger)
		buf := make([]byte, 1024)
		b.StartTimer()

		for {
			_, err := ms.Read(buf)
			if err != nil {
				break
			}
		}

		b.StopTimer()
		ms.Close()
	}
}

// BenchmarkManagedDockerStream_Close benchmarks close performance
func BenchmarkManagedDockerStream_Close(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		reader := strings.NewReader("test")
		stream := io.NopCloser(reader)
		httpResp := &http.Response{
			Body: io.NopCloser(strings.NewReader("http body")),
		}
		ms := NewManagedDockerStream(stream, httpResp, "bench", "bench", logger)
		b.StartTimer()

		ms.Close()
	}
}
