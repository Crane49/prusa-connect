package httpapi

// connectorEndpointExamples are the worked examples for the addresses this
// server dials. They are in this file because examples.go is at the length
// limit this project holds a source file to.
func connectorEndpointExamples() map[string]operationExample {
	return map[string]operationExample{
		"list-connector-endpoints": {Response: map[string]any{
			"items": []ConnectorEndpoint{exampleConnectorEndpoint()},
		}},

		"put-connector-endpoint": {
			Headers:  map[string]string{"If-Match": ifMatchVersion},
			Request:  connectorEndpointBody{EndpointSpec: exampleConnectorEndpoint().EndpointSpec},
			Response: exampleConnectorEndpoint(),
		},
	}
}

// exampleConnectorEndpoint is one declared address: a Moonraker host on the
// workshop network, switched on, with one dialect option.
func exampleConnectorEndpoint() ConnectorEndpoint {
	return ConnectorEndpoint{
		EndpointSpec: EndpointSpec{
			Connector: "moonraker",
			Address:   "192.168.1.42:7125",
			Enabled:   true,
			Options:   map[string]string{"api_key": "none"},
		},
		Name:    exampleEndpointName,
		Version: "v7",
	}
}
