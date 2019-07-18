package main

import (
	"github.com/PremiereGlobal/vault-to-envs/pkg/vaulttoenvs"
	"github.com/sirupsen/logrus"
)

var log *logrus.Logger

func main() {

	log = logrus.New()

	// Initializes configuration (command line interface, parameters, etc.)
	initializeCommandParameters()

	app.Execute()
}

func run() {
	if config.GetBool("debug") == true {
		log.SetLevel(logrus.DebugLevel)
		log.Debug("Debug level set")
	} else {
		log.SetLevel(logrus.InfoLevel)
	}

	v2eConfig := &vaulttoenvs.Config{
		VaultAddr:        config.GetString("vault-address"),
		Debug:            config.GetBool("debug"),
		SecretConfig:     config.GetString("secret-config"),
		SecretConfigFile: config.GetString("secret-config-file"),
	}

	if v2eConfig.VaultAddr == "" {
		log.Fatal("--vault-address must be provided (or env var VAULT_ADDR)")
	}

	if config.GetString("vault-token") == "" {
		log.Fatal("--vault-token must be provided (or env var VAULT_TOKEN)")
	}

	if v2eConfig.SecretConfig == "" && v2eConfig.SecretConfigFile == "" {
		log.Fatal("--secret-config or --secret-config-file must be provided (or env var SECRET_CONFIG or SECRET_CONFIG_FILE)")
	}

	if v2eConfig.SecretConfig != "" && v2eConfig.SecretConfigFile != "" {
		log.Fatal("Only one of --secret-config OR --secret-config-file can be set")
	}

	log.Debugf("Vault Address: %s", v2eConfig.VaultAddr)
	log.Debugf("Debug: %v", v2eConfig.Debug)
	log.Debugf("Secret Config: %s", v2eConfig.SecretConfig)
	log.Debugf("Secret Config File: %s", v2eConfig.SecretConfigFile)

	v2e := vaulttoenvs.NewVaultToEnvs(v2eConfig)
	v2e.SetLogger(log)
	v2e.SetVaultToken(config.GetString("vault-token"))

	err := v2e.DisplayEnvExports()
	if err != nil {
		log.Fatal(err)
	}
}
