package abis

import _ "embed"

//go:embed EntryPoint.json
var EntryPointJSON []byte

//go:embed EntryPointSimulations.json
var EntryPointSimulationsJSON []byte

//go:embed SimpleAccountFactory.json
var SimpleAccountFactoryJSON []byte

//go:embed EntryPointSimulations.bytecode
var EntryPointSimulationsDeployedBytecode []byte

