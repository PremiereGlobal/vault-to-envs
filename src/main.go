package main

import (
  "fmt"
  "encoding/json"
  "regexp"
  "strings"
  log "github.com/Sirupsen/logrus"
  envconfig "github.com/kelseyhightower/envconfig"
  VaultApi "github.com/hashicorp/vault/api"
)

type Specification struct {
    Vault_Addr  string `required:"true"`
    Vault_Token    string `required:"true"`
    Debug         string `default:"false"`
    Secret_Config  string `required:"true"`
}

type SecretConfig struct {
    ConfigItems []ConfigItem
}

type ConfigItem struct {
  SecretPath string
  TTL int
  EnvKeyMap []EnvKeyMap
}

type EnvKeyMap struct {
  EnvVar string
  VaultKey string
}

var VaultClient *VaultApi.Client
var Vault *VaultApi.Logical
var VaultSys *VaultApi.Sys

func main() {

  // General-use error handlers
  var err error
  var ok bool

  // Ensure we have the right ENVs set
  var spec Specification
  err = envconfig.Process("", &spec)
  if err != nil {
      log.Fatal(err.Error())
  }

  // Set log level
  if spec.Debug == "true" {
    log.SetLevel(log.DebugLevel)
    log.Debug("Debug level set")
  } else {
    log.SetLevel(log.WarnLevel)
  }

  // Configure new Vault Client
  conf := &VaultApi.Config{Address: spec.Vault_Addr}
  VaultClient, _ = VaultApi.NewClient(conf)
  VaultClient.SetToken(spec.Vault_Token)

  // Define a Logical Vault client (to read/write values)
  Vault = VaultClient.Logical()
  VaultSys = VaultClient.Sys()

  // This holds all of the secret configuration maps
  secretConfig := SecretConfig{}

  // Unmarshal the SECRET_CONFIG json
  var f interface{}
  err = json.Unmarshal([]byte(spec.Secret_Config), &f)
  if err != nil {
  log.Fatal("Error parsing SECRET_CONFIG: ", err)
  }

  // Iterate over the top level array to get mappings
  jsonItems := f.([]interface{})
  for _, v := range jsonItems {

  // Use type assertions to ensure that the value is a JSON object
  switch jsonItems := v.(type) {

      // The value is an configItem, represented as a generic interface
      case interface{}:

        // Create a new ConfigItem which will hold the details for this secret
        var configItem ConfigItem

        // Access the values in the JSON object and ensure they are of the right structure
        var configItemArray map[string]interface{}
        configItemArray, ok = jsonItems.(map[string]interface{})
        if(!ok) {
          log.Fatal("SECRET_CONFIG has invalid structure")
        }

        // Iterate over each key/value within the config item
        for itemKey, itemValue := range configItemArray {
          switch itemKey {
            case "vault_path":

              // Make sure is a string
              switch itemValue := itemValue.(type) {
                case string:
                  configItem.SecretPath = itemValue
                default:
                  log.Fatal("SECRET_CONFIG item 'vault_path' should be a string")
              }
            case "ttl":

              // Make sure that TTL is a number; all numbers are transformed to float64
              switch itemValue := itemValue.(type) {
              case float64:
                  configItem.TTL = int(itemValue)
                default:
                  log.Fatal("SECRET_CONFIG item 'ttl' should be a integer")
              }
            case "set":

              // Make sure that "set" is an interface{} (JSON object)"
              switch setObj := itemValue.(type) {

                case interface{}:

                  // Ensure that the set value has the correct type
                  var setObjArray map[string]interface{}
                  setObjArray, ok = setObj.(map[string]interface{})
                  if(!ok) {
                    log.Fatal("SECRET_CONFIG item 'set' has invalid structure")
                  }

                  // Iterate over each env/key pair and create an envKeyMap
                  for envVar, vaultKey := range setObjArray {

                    // Create a new EnvKeyMap to hold our env/key values
                    var envKeyMap EnvKeyMap

                    // Ensure the the vault key is a string
                    switch vaultKey := vaultKey.(type) {
                      case string:

                        // Ensure env variable passed doesn't have any invalid characters
                        reg, err := regexp.Compile(`^[a-zA-Z_]{1}[a-zA-Z0-9_]*$`)
                        if err != nil {
                          log.Fatal("Error parsing regex", err)
                        }
                        if !reg.MatchString(envVar) {
                          log.Fatal("Invalid environment variable name '", envVar, "'")
                        }

                        // Set our env/key pairs
                        envKeyMap.EnvVar = envVar
                        envKeyMap.VaultKey = vaultKey
                        configItem.EnvKeyMap = append(configItem.EnvKeyMap, envKeyMap)

                      default:
                        fmt.Println("Incorrect type for '", envVar, "' expected string")
                    }
                  }
                default:
                  log.Fatal("SECRET_CONFIG structure invalid")
              }
            default:
              // Ignore any extra fields that we don't care about
          }
        }
        secretConfig.ConfigItems = append(secretConfig.ConfigItems, configItem)

      // Not a JSON object; handle the error
      default:
        log.Fatal("SECRET_CONFIG structure invalid")
    }
  }

  log.Debug("secretConfig", secretConfig)

  // Retrieve the secrets from Vault and display them
  for _, configItem := range secretConfig.ConfigItems {
    DisplaySecretEnv(&configItem)
  }

}

func DisplaySecretEnv(configItem *ConfigItem) {

  // Read the secret from Vault
  secret, err := Vault.Read(configItem.SecretPath)
  if err != nil {
    log.Fatal("Error fetching secret: ", err.Error())
  }

  // If we got back an empty response, fail
  if secret == nil {
    log.Fatal("Could not find secret ", configItem.SecretPath)
  }

  // Ensure that secret is renewable if trying to set the TTL
  if configItem.TTL != 0 && !secret.Renewable  {
    log.Fatal("Cannot set TTL on secret ", configItem.SecretPath, ". TTL can only be set on dynamic secrets like AWS credentials")
  } else if configItem.TTL != 0 {
    log.Info("Renewing lease on ", configItem.SecretPath, " to ", configItem.TTL, " seconds")
    log.Info("Original Lease Info ", secret.LeaseID, ",", secret.LeaseDuration)
    renewed_secret, err := VaultSys.Renew(secret.LeaseID, configItem.TTL)
    if err != nil {
      log.Fatal("Error renewing secret (setting TTL): ", err.Error())
    }
    log.Info("New Lease Info ", renewed_secret.LeaseID, ",", renewed_secret.LeaseDuration)

    // Check if lease duration was able to be set to desired amount
    // Added some tolerance for any request delay
    if (configItem.TTL-renewed_secret.LeaseDuration) > 5 {
      log.Fatal("Not able to set TTL to desired amount. Desired:", configItem.TTL, "; Actual:", renewed_secret.LeaseDuration)
    }
  }

  // Display Environment variable output
  for _, envKeyMap := range configItem.EnvKeyMap {

    if secret.Data[envKeyMap.VaultKey] == nil {
      log.Fatal("Key '", envKeyMap.VaultKey, "' not found in secret '", configItem.SecretPath, "'")
    }

    // Prints the env variable line to stdout
    // Single quotes value and escapes single quotes in secret with '"'"'
    fmt.Println("export "+envKeyMap.EnvVar+"='"+strings.Replace(secret.Data[envKeyMap.VaultKey].(string), "'", "'\"'\"'", -1)+"'")
  }

  // TODO: Zero out the secret from memory
  // TODO: Revoke dynamic secrets on failure
}
