package gql

// Alias unwieldy types from GraphQL generated code
type AddOn = CreateAddOnCreateAddOnCreateAddOnPayloadAddOn
type AddOnOptions map[string]interface{}
type LimitedAccessTokenOptions map[string]interface{}

var AddOnTypes = map[string]AddOnType{
	"logtail":       AddOnTypeLogtail,
	"upstash_redis": AddOnTypeUpstashRedis,
}
