package enrichment

import (
	"encoding/json"
	"github.com/spf13/viper"
	"testing"

	"github.com/jitsucom/jitsu/server/appconfig"
	"github.com/jitsucom/jitsu/server/geo"
	"github.com/jitsucom/jitsu/server/jsonutils"
	"github.com/jitsucom/jitsu/server/test"
	"github.com/stretchr/testify/require"
)

func TestIpLookup(t *testing.T) {
	viper.Set("server.log.path", "")

	geoDataMock := &geo.Data{
		Country: "US",
		City:    "New York",
		Lat:     79.00,
		Lon:     22.00,
		Zip:     "14101",
		Region:  "",
	}
	tests := []struct {
		name        string
		source      string
		destination string
		input       map[string]interface{}
		expected    map[string]interface{}
	}{
		{
			"Nil input object",
			"/key1",
			"/key2",
			nil,
			nil,
		},
		{
			"Empty input object",
			"/key1",
			"/key2",
			map[string]interface{}{},
			map[string]interface{}{},
		},
		{
			"Object with string instead of ip",
			"/ip",
			"/parsed_ip",
			map[string]interface{}{"ip": "abc"},
			map[string]interface{}{"ip": "abc"},
		},
		{
			"Object with wrong format",
			"/ip",
			"/parsed_ip",
			map[string]interface{}{"ip": 10},
			map[string]interface{}{"ip": 10},
		},
		{
			"Object with unknown ip",
			"/ip",
			"/parsed_ip",
			map[string]interface{}{"ip": "20.20.20.20"},
			map[string]interface{}{"ip": "20.20.20.20"},
		},
		{
			"Object with ip but result node wrong format",
			"/ip",
			"/parsed_ip/payload",
			map[string]interface{}{"ip": "10.10.10.10", "parsed_ip": "abc"},
			map[string]interface{}{"ip": "10.10.10.10", "parsed_ip": "abc"},
		},
		{
			"Object with ip ok",
			"/ip",
			"/parsed_ip",
			map[string]interface{}{"ip": "10.10.10.10"},
			map[string]interface{}{"ip": "10.10.10.10", "parsed_ip": map[string]interface{}{"city": "New York", "country": "US", "latitude": json.Number("79"), "longitude": json.Number("22"), "zip": "14101"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			appconfig.Init(false)
			appconfig.Instance.GeoResolver = geo.Mock{"10.10.10.10": geoDataMock}

			ipRule, err := NewIpLookupRule(jsonutils.NewJsonPath(tt.source), jsonutils.NewJsonPath(tt.destination))
			require.NoError(t, err)

			ipRule.Execute(tt.input)
			test.ObjectsEqual(t, tt.expected, tt.input, "Events aren't equal")
		})
	}
}
