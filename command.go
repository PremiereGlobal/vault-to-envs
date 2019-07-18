package main

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var app *cobra.Command
var config *viper.Viper

func initializeCommandParameters() {

	config = viper.New()

	var cmdRoot = &cobra.Command{
		Use:   "v2e",
		Short: "Utility for extracting Vault secrets into environment variables",
		Long:  `Utility for extracting Vault secrets into environment variables using a secrets definition`,
		Run: func(cmd *cobra.Command, args []string) {
			run()
		},
	}

	app = cmdRoot

	app.PersistentFlags().StringP("vault-address", "", "", "Vault address (ex: https://vault.my-domain.com:8200)")
	config.BindPFlag("vault-address", app.PersistentFlags().Lookup("vault-address"))
	config.BindEnv("vault-address", "VAULT_ADDR")

	app.PersistentFlags().StringP("vault-token", "", "", "Vault token")
	config.BindPFlag("vault-token", app.PersistentFlags().Lookup("vault-token"))
	config.BindEnv("vault-token", "VAULT_TOKEN")

	app.PersistentFlags().StringP("secret-config", "", "", "The secret config string to use")
	config.BindPFlag("secret-config", app.PersistentFlags().Lookup("secret-config"))
	config.BindEnv("secret-config", "SECRET_CONFIG")

	app.PersistentFlags().StringP("secret-config-file", "", "", "The secret config file to use (takes precedence over --secret-config)")
	config.BindPFlag("secret-config-file", app.PersistentFlags().Lookup("secret-config-file"))
	config.BindEnv("secret-config-file", "SECRET_CONFIG_FILE")

	app.PersistentFlags().BoolP("debug", "d", false, "Show debug output")
	config.BindPFlag("debug", app.PersistentFlags().Lookup("debug"))
	config.BindEnv("debug", "DEBUG")

}
