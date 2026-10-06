package contentpacks

import "fmt"

// NetFlow v5/v9 and IPFIX share the official netflow scheme; sFlow uses sflow.
func buildOTelFlowReceiver(source ResolvedSource, recipe CollectorRecipe) ([]otelReceiverBuild, string, error) {
	config := cloneOTelConfig(recipe.Config)
	scheme := stringConfig(config, "scheme")
	if scheme == "" {
		scheme = "netflow"
	}
	if scheme != "netflow" && scheme != "sflow" {
		return nil, "", fmt.Errorf("flow scheme must be netflow or sflow")
	}
	if raw, ok := config["send_raw"]; ok && raw != false {
		return nil, "", fmt.Errorf("network flow identity requires decoded records")
	}
	config["scheme"] = scheme
	if _, ok := config["port"]; !ok {
		if scheme == "sflow" {
			config["port"] = 6343
		} else {
			config["port"] = 2055
		}
	}
	if _, ok := config["sockets"]; !ok {
		config["sockets"] = 1
	}
	if _, ok := config["workers"]; !ok {
		config["workers"] = 2
	}
	return []otelReceiverBuild{{ID: otelReceiverID("netflow", source.Source.SourceID), Type: "netflow", Config: config, Warnings: []string{"Restrict UDP listeners to the site's exporter IPs; transport sender bindings resolve target identity. sFlow counter samples and custom fields are not supported by the upstream receiver."}}}, "logs", nil
}
