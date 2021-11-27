//go:build integration
// +build integration

package alphaart

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func Test_Client_GetActivityHistory(t *testing.T) {
	before := time.Date(2021, 11, 26, 0, 0, 0, 0, time.UTC)

	for _, tc := range []struct{
		desc string
		params *QueryParam
		chk func(t *testing.T, history *ActivityHistory)
	} {
		{
			desc: "Happy path - default params",
			chk: func(t *testing.T, history *ActivityHistory) {
				require.NotNil(t, history)
				assert.Len(t, history.Activities, 20)
			},
		},
		{
			desc: "Happy path - with limit",
			params: &QueryParam{
				Limit:        1,
				TradingTypes: []TradingType{Sale, Listing, Offer},
			},
			chk: func(t *testing.T, history *ActivityHistory) {
				require.NotNil(t, history)
				assert.Len(t, history.Activities, 1)
			},
		},
		{
			desc: "Happy path - with trading type",
			params: &QueryParam{
				TradingTypes: []TradingType{Listing},
			},
			chk: func(t *testing.T, history *ActivityHistory) {
				require.NotNil(t, history)
				assert.Len(t, history.Activities, 20)

				for i := range history.Activities {
					assert.Equal(t, Listing, history.Activities[i].TradingType)
				}
			},
		},
		{
			desc: "Happy path - with before",
			params: &QueryParam{
				TradingTypes: []TradingType{Sale, Listing, Offer},
				Before:       &before,
				Limit: 3,
			},
			chk: func(t *testing.T, history *ActivityHistory) {
				require.NotNil(t, history)
				assert.Len(t, history.Activities, 3)

				for i := range history.Activities {
					assert.True(t, history.Activities[i].CreatedAt.Before(before), "createdAt(%s)", history.Activities[i].CreatedAt.String())
				}
			},
		},
	}{
		{
			t.Run(tc.desc, func(t *testing.T) {
				c, err := NewClient(zap.NewNop())
				require.NoError(t, err)

				history, err := c.GetActivityHistory(badBromotoesCollectionID, tc.params)
				require.NoError(t, err)

				tc.chk(t, history)
			})
		}

		// sleep so we dont hammer the api
		time.Sleep(time.Millisecond * 500)
	}
}
