package ui

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestAgo(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "just now", ago(10*time.Second))
	assert.Equal(t, "1 min ago", ago(90*time.Second))
	assert.Equal(t, "5 mins ago", ago(5*time.Minute))
	assert.Equal(t, "3 hours ago", ago(3*time.Hour))
}

func TestAge(t *testing.T) {
	t.Parallel()

	assert.Empty(t, age(time.Time{}))
	assert.Equal(t, "5m", age(time.Now().Add(-5*time.Minute-time.Second)))
	assert.Equal(t, "2d", age(time.Now().Add(-49*time.Hour)))
}

func TestFooterLineShowsLastUpdated(t *testing.T) {
	t.Parallel()

	m := loadedModel()
	m.width = 80
	m.lastUpdated = time.Now().Add(-5*time.Minute - time.Second)
	assert.Contains(t, m.footerLine(), "last updated 5 mins ago")
}
