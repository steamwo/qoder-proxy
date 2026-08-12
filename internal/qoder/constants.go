package qoder

const (
	BaseURL       = "https://api3.qoder.sh"
	ModelsPath    = "/algo/api/v2/model/list"
	ChatPath      = "/algo/api/v2/service/pro/sse/agent_chat_generation?FetchKeys=llm_model_result&AgentId=agent_common"
	LoginURL      = "https://qoder.com/device/selectAccounts"
	PollURL       = "https://openapi.qoder.sh/api/v1/deviceToken/poll"
	UserInfoURL   = "https://openapi.qoder.sh/api/v1/userinfo"
	DefaultAgent  = "agent_common"
	DefaultTask   = "common"
	ClientVersion = "1.0.0"
	UserAgent     = "qoder-proxy/0.1.0"
)
