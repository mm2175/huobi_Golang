package account

import (
	"encoding/json"

	"github.com/huobirdcenter/huobi_golang/pkg/model/base"
)

type SubscribeAccountV2Response struct {
	base.WebSocketV2ResponseBase
	Data *struct {
		Currency    string `json:"currency"`
		AccountId   int    `json:"accountId"`
		Balance     string `json:"balance"`
		Available   string `json:"available"`
		ChangeType  string `json:"changeType"`
		AccountType string `json:"accountType"`
		ChangeTime  int64  `json:"changeTime"`
	}
}

func (s *SubscribeAccountV2Response) String() string {
	bytes, err := json.Marshal(s)
	if err != nil {
		return "marshal"
	}

	return string(bytes)
}
