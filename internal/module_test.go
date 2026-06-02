package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewStreamValidator_SecureByDefault(t *testing.T) {
	t.Run("no secret and no opt-in refuses to start", func(t *testing.T) {
		t.Setenv("TALOS_STREAM_HMAC_SECRET", "")
		t.Setenv("TALOS_WS_ALLOW_INSECURE", "")

		v, err := newStreamValidator()
		require.ErrorIs(t, err, errInsecureStream)
		assert.Nil(t, v)
	})

	t.Run("no secret with explicit insecure opt-in disables auth", func(t *testing.T) {
		t.Setenv("TALOS_STREAM_HMAC_SECRET", "")
		t.Setenv("TALOS_WS_ALLOW_INSECURE", "1")

		v, err := newStreamValidator()
		require.NoError(t, err)
		assert.Nil(t, v, "nil validator = auth disabled")
	})

	t.Run("secret configured enables auth regardless of opt-in", func(t *testing.T) {
		t.Setenv("TALOS_STREAM_HMAC_SECRET", "super-secret-shared-key")
		t.Setenv("TALOS_WS_ALLOW_INSECURE", "")

		v, err := newStreamValidator()
		require.NoError(t, err)
		assert.NotNil(t, v)
	})

	t.Run("non-1 opt-in value is treated as insecure-disabled", func(t *testing.T) {
		t.Setenv("TALOS_STREAM_HMAC_SECRET", "")
		t.Setenv("TALOS_WS_ALLOW_INSECURE", "true") // only "1" opts in

		_, err := newStreamValidator()
		require.ErrorIs(t, err, errInsecureStream)
	})
}

func TestWSAllowInsecure(t *testing.T) {
	t.Run("unset is false", func(t *testing.T) {
		t.Setenv("TALOS_WS_ALLOW_INSECURE", "")
		assert.False(t, wsAllowInsecure())
	})
	t.Run("only 1 opts in", func(t *testing.T) {
		t.Setenv("TALOS_WS_ALLOW_INSECURE", "1")
		assert.True(t, wsAllowInsecure())
		t.Setenv("TALOS_WS_ALLOW_INSECURE", "yes")
		assert.False(t, wsAllowInsecure())
	})
}

func TestRetentionMonths(t *testing.T) {
	t.Run("unset disables retention", func(t *testing.T) {
		t.Setenv("TALOS_RETENTION_MONTHS", "")
		assert.Equal(t, 0, retentionMonths())
	})
	t.Run("negative is clamped to disabled", func(t *testing.T) {
		t.Setenv("TALOS_RETENTION_MONTHS", "-3")
		assert.Equal(t, 0, retentionMonths())
	})
	t.Run("valid positive is honoured", func(t *testing.T) {
		t.Setenv("TALOS_RETENTION_MONTHS", "12")
		assert.Equal(t, 12, retentionMonths())
	})
}
