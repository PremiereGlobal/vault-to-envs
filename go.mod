module github.com/PremiereGlobal/vault-to-envs

go 1.12

replace github.com/PremiereGlobal/vault-to-envs => ./

require (
	github.com/aws/aws-sdk-go v1.20.20
	github.com/hashicorp/vault/api v1.0.2
	github.com/sirupsen/logrus v1.4.2
	github.com/spf13/cobra v0.0.5
	github.com/spf13/viper v1.4.0
)
