package requester

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test data structures
type TestData struct {
	Field1 int
	Field2 string
}

type TestStruct struct {
	Value int
}

// Helper function to check if log output contains a specific message
func containsLogMessage(logOutput, message string) bool {
	return bytes.Contains([]byte(logOutput), []byte(message))
}

func TestHandleCall_BothSuccess_SameResult(t *testing.T) {
	local := func() (int, error) {
		time.Sleep(10 * time.Millisecond)
		return 42, nil
	}

	remote := func() (int, error) {
		time.Sleep(20 * time.Millisecond)
		return 42, nil
	}

	var buf bytes.Buffer
	logger := zerolog.New(&buf).With().Timestamp().Logger()

	result, err := handleCall(local, remote, logger)
	require.NoError(t, err)
	assert.Equal(t, 42, result)

	logOutput := buf.String()
	assert.NotContains(t, logOutput, "error")
}

func TestHandleCall_BothSuccess_DifferentResult(t *testing.T) {
	local := func() (int, error) {
		time.Sleep(10 * time.Millisecond)
		return 42, nil
	}

	remote := func() (int, error) {
		time.Sleep(20 * time.Millisecond)
		return 43, nil
	}

	var buf bytes.Buffer
	logger := zerolog.New(&buf).With().Timestamp().Logger()

	result, err := handleCall(local, remote, logger)
	require.NoError(t, err)
	assert.Equal(t, 43, result)

	logOutput := buf.String()
	assert.Contains(t, logOutput, "results from local and remote client are not the same")
}

func TestHandleCall_LocalSuccess_RemoteFail(t *testing.T) {
	local := func() (int, error) {
		time.Sleep(10 * time.Millisecond)
		return 42, nil
	}

	remote := func() (int, error) {
		time.Sleep(20 * time.Millisecond)
		return 0, errors.New("remote error")
	}

	var buf bytes.Buffer
	logger := zerolog.New(&buf).With().Timestamp().Logger()

	result, err := handleCall(local, remote, logger)
	require.NoError(t, err)
	assert.Equal(t, 42, result)

	logOutput := buf.String()
	assert.Contains(t, logOutput, "error from remote client but not from local client")
}

func TestHandleCall_LocalFail_RemoteSuccess(t *testing.T) {
	local := func() (int, error) {
		time.Sleep(10 * time.Millisecond)
		return 0, errors.New("local error")
	}

	remote := func() (int, error) {
		time.Sleep(20 * time.Millisecond)
		return 43, nil
	}

	var buf bytes.Buffer
	logger := zerolog.New(&buf).With().Timestamp().Logger()

	result, err := handleCall(local, remote, logger)
	require.NoError(t, err)
	assert.Equal(t, 43, result)

	logOutput := buf.String()
	assert.Contains(t, logOutput, "error from local client but not from remote client")
}

func TestHandleCall_BothFail_SameError(t *testing.T) {
	local := func() (int, error) {
		time.Sleep(10 * time.Millisecond)
		return 0, errors.New("common error")
	}

	remote := func() (int, error) {
		time.Sleep(20 * time.Millisecond)
		return 0, errors.New("common error")
	}

	var buf bytes.Buffer
	logger := zerolog.New(&buf).With().Timestamp().Logger()

	_, err := handleCall(local, remote, logger)
	require.Error(t, err)
	assert.Equal(t, "common error", err.Error())

	logOutput := buf.String()
	assert.NotContains(t, logOutput, "errors from local and remote client are not the same")
}

func TestHandleCall_BothFail_DifferentErrors(t *testing.T) {
	local := func() (int, error) {
		time.Sleep(10 * time.Millisecond)
		return 0, errors.New("local error")
	}

	remote := func() (int, error) {
		time.Sleep(20 * time.Millisecond)
		return 0, errors.New("remote error")
	}

	var buf bytes.Buffer
	logger := zerolog.New(&buf).With().Timestamp().Logger()

	_, err := handleCall(local, remote, logger)
	require.Error(t, err)
	assert.Equal(t, "remote error", err.Error())

	logOutput := buf.String()
	assert.Contains(t, logOutput, "errors from local and remote client are not the same")
}

func TestHandleCall_StructType_BothSuccess_SameResult(t *testing.T) {
	local := func() (TestData, error) {
		time.Sleep(10 * time.Millisecond)
		return TestData{Field1: 1, Field2: "test"}, nil
	}

	remote := func() (TestData, error) {
		time.Sleep(20 * time.Millisecond)
		return TestData{Field1: 1, Field2: "test"}, nil
	}

	var buf bytes.Buffer
	logger := zerolog.New(&buf).With().Timestamp().Logger()

	result, err := handleCall(local, remote, logger)
	require.NoError(t, err)
	expected := TestData{Field1: 1, Field2: "test"}
	assert.Equal(t, expected, result)

	logOutput := buf.String()
	assert.NotContains(t, logOutput, "error")
}

func TestHandleCall_StructType_BothSuccess_DifferentResult(t *testing.T) {
	local := func() (TestData, error) {
		time.Sleep(10 * time.Millisecond)
		return TestData{Field1: 1, Field2: "test"}, nil
	}

	remote := func() (TestData, error) {
		time.Sleep(20 * time.Millisecond)
		return TestData{Field1: 2, Field2: "test"}, nil
	}

	var buf bytes.Buffer
	logger := zerolog.New(&buf).With().Timestamp().Logger()

	result, err := handleCall(local, remote, logger)
	require.NoError(t, err)
	expected := TestData{Field1: 2, Field2: "test"}
	assert.Equal(t, expected, result)

	logOutput := buf.String()
	assert.Contains(t, logOutput, "results from local and remote client are not the same")
}

func TestHandleCall_PointerType_LocalNil_RemoteNonNil(t *testing.T) {
	local := func() (*TestStruct, error) {
		time.Sleep(10 * time.Millisecond)
		return nil, nil
	}

	remote := func() (*TestStruct, error) {
		time.Sleep(20 * time.Millisecond)
		return &TestStruct{Value: 1}, nil
	}

	var buf bytes.Buffer
	logger := zerolog.New(&buf).With().Timestamp().Logger()

	result, err := handleCall(local, remote, logger)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, result.Value)

	logOutput := buf.String()
	assert.Contains(t, logOutput, "results from local and remote client are not the same")
}

func TestHandleCall_PointerType_LocalNonNil_RemoteNil(t *testing.T) {
	local := func() (*TestStruct, error) {
		time.Sleep(10 * time.Millisecond)
		return &TestStruct{Value: 1}, nil
	}

	remote := func() (*TestStruct, error) {
		time.Sleep(20 * time.Millisecond)
		return nil, nil
	}

	var buf bytes.Buffer
	logger := zerolog.New(&buf).With().Timestamp().Logger()

	result, err := handleCall(local, remote, logger)
	require.NoError(t, err)
	require.Nil(t, result)

	logOutput := buf.String()
	assert.Contains(t, logOutput, "results from local and remote client are not the same")
}
