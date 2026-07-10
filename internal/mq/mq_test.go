package mq

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestURIFromEnv(t *testing.T) {
	t.Run("builds the URI and escapes URL-special characters in the password", func(t *testing.T) {
		t.Setenv("RABBITMQ_USERNAME", "user")
		t.Setenv("RABBITMQ_PASSWORD", "p@ss:w/rd")
		t.Setenv("RABBITMQ_HOST", "broker")
		t.Setenv("RABBITMQ_PORT", "5673")
		t.Setenv("RABBITMQ_VHOST", "apps")

		env := &Env{}
		uri := URIFromEnv(env)
		require.NoError(t, env.Err())
		assert.Equal(t, "amqp://user:p%40ss%3Aw%2Frd@broker:5673/apps", uri)
	})

	t.Run("errors naming every required var that is unset", func(t *testing.T) {
		// A clean slate: unset every connection var (t.Setenv restores them after the test).
		for _, k := range []string{"RABBITMQ_USERNAME", "RABBITMQ_PASSWORD", "RABBITMQ_HOST", "RABBITMQ_PORT", "RABBITMQ_VHOST"} {
			t.Setenv(k, "")
		}
		env := &Env{}
		_ = URIFromEnv(env)
		err := env.Err()
		require.Error(t, err)
		for _, k := range []string{"RABBITMQ_USERNAME", "RABBITMQ_PASSWORD", "RABBITMQ_HOST", "RABBITMQ_PORT", "RABBITMQ_VHOST"} {
			assert.Contains(t, err.Error(), k)
		}
	})
}
