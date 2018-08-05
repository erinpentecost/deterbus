package deterbus_test

import (
	"testing"

	"github.com/erinpentecost/deterbus/deterbus"
)

func TestConstructor(t *testing.T) {
	b := deterbus.New()
	b.Stop()
}
