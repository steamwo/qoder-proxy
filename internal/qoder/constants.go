package qoder

const (
	BaseURL                 = "https://api3.qoder.sh"
	DefaultInferenceBaseURL = "https://api2.qoder.sh"
	ModelsPath      = "/algo/api/v2/model/list"
	ChatPath        = "/algo/api/v2/service/pro/sse/agent_chat_generation?FetchKeys=llm_model_result&AgentId=agent_common"
	ChatEncodedPath = ChatPath + "&Encode=1"
	LoginURL        = "https://qoder.com/device/selectAccounts"
	PollURL         = "https://openapi.qoder.sh/api/v1/deviceToken/poll"
	RefreshURL      = "https://openapi.qoder.sh/api/v1/deviceToken/refresh"
	UserInfoURL     = "https://openapi.qoder.sh/api/v1/userinfo"
	DefaultAgent    = "agent_common"
	DefaultTask     = "common"
	LegacyClientVersion = "1.0.0"
	InferProtocolVersion = "1.1.34"
	ProductionClientID   = "e883ade2-e6e3-4d6d-adf7-f92ceff5fdcb"
	UserAgent            = "qoder/" + InferProtocolVersion
)
