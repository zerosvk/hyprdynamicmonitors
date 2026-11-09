package power

import (
	"testing"

	"github.com/fiffeek/hyprdynamicmonitors/internal/config"
	"github.com/fiffeek/hyprdynamicmonitors/internal/utils"
	"github.com/godbus/dbus/v5"
	"github.com/stretchr/testify/assert"
)

func TestSignalMatchesFilters(t *testing.T) {
	sig := &dbus.Signal{
		Name: "org.freedesktop.DBus.Properties.PropertiesChanged",
		Body: []interface{}{"org.freedesktop.UPower.Device", map[string]dbus.Variant{"Online": dbus.MakeVariant(true)}},
	}

	tests := []struct {
		name    string
		filters []*config.DbusSignalReceiveFilter
		want    bool
	}{
		{name: "no filters", filters: nil, want: false},
		{
			name:    "name matches",
			filters: []*config.DbusSignalReceiveFilter{{Name: utils.StringPtr(sig.Name)}},
			want:    true,
		},
		{
			name:    "name mismatches",
			filters: []*config.DbusSignalReceiveFilter{{Name: utils.StringPtr("other")}},
			want:    false,
		},
		{
			// used to panic with a nil pointer dereference in the power detector
			name:    "body only matches",
			filters: []*config.DbusSignalReceiveFilter{{Body: utils.StringPtr("Online")}},
			want:    true,
		},
		{
			name:    "body only mismatches",
			filters: []*config.DbusSignalReceiveFilter{{Body: utils.StringPtr("LidIsClosed")}},
			want:    false,
		},
		{
			name: "name and body must both match",
			filters: []*config.DbusSignalReceiveFilter{
				{Name: utils.StringPtr(sig.Name), Body: utils.StringPtr("LidIsClosed")},
			},
			want: false,
		},
		{
			name: "any filter matching is enough",
			filters: []*config.DbusSignalReceiveFilter{
				{Name: utils.StringPtr("other")},
				{Body: utils.StringPtr("Online")},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, signalMatchesFilters(sig, tt.filters))
		})
	}
}
