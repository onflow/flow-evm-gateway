## EVM E2E Gateway Tests

EVM gateway end-to-end tests use web3.js client to interact with the evm gateway.
Web3.js client is used to ensure 100% compliance with the JSON-RPC API specification, 
and it allows us to detect any mistakes in how the data is returned from the API.

### Running Tests
Running the test is done simply by running the test file:
```
go test ./e2e_web3js_test.go
```

#### Adding new E2E tests
Adding a new test is done simply by adding a new JS test file to the web3js folder and then 
adding execution of that test in the e2e_web3js_test.go. 

❗️ Keep in mind that if you want to have 
an isolated and new instance of the gateway you need to separate tests in a new file. Each test 
file will be run with a fresh instance of evm gateway, but tests that are written in the same file 
will share the evm gateway instance and any state established by the test.

**Example:**

Let's say we want to add a new e2e test called `getBlock` we add a new file to `web3js/getBlock.js`. 
Inside that file we define any assertions we want to make, then we add this test 
to the `e2e_web3js_test.go` like so:
```go
func Test_Web3Acceptance(t *testing.T) {
	runTest(t, "getBlock")
}
```

### Producing fixtures
Files in the `fixtures` folder can be produced by using a `solc` compiler or simply by 
using [remix](https://remix.ethereum.org/) online IDE. You can simply paste the `test.sol`, 
change it the way you see fit and compile, which will produce ABI JSON and compiled binary. 
You then replace contents of `test-abi.json` with new ABI and `test.bin` with the compiled binary.