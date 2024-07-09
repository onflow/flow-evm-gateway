const { assert } = require('chai')
const conf = require('./config')
const helpers = require('./helpers')
const web3 = conf.web3

it('returns a null result for missing filter', async () => {
  // check that null is returned when the filter could not be found.
  let response = await helpers.callRPCMethod('eth_getFilterLogs', ['0xffa1'])

  assert.equal(200, response.status)
  assert.isUndefined(response.body['result'])
})

it('create logs filter and call eth_getFilterLogs', async () => {
  // deploy contract
  let deployed = await helpers.deployContract('storage')
  let contractAddress = deployed.receipt.contractAddress

  // create logs filter on the address of the deployed contract
  let response = await helpers.callRPCMethod('eth_newFilter', [{ 'address': contractAddress }])

  assert.equal(200, response.status)
  let filterID = response.body.result
  assert.isDefined(filterID)

  // make contract function call that emits a log
  let res = await helpers.signAndSend({
    from: conf.eoa.address,
    to: contractAddress,
    data: deployed.contract.methods.sum(15, 20).encodeABI(),
    gas: 1000000,
    gasPrice: 0
  })

  assert.equal(res.receipt.status, conf.successStatus)

  // check the matching items from the logs filter
  response = await helpers.callRPCMethod('eth_getFilterLogs', [filterID])
  assert.equal(200, response.status)
  assert.isUndefined(response.body.error)

  let logs = response.body.result
  assert.isDefined(logs)
  assert.equal(1, logs.length)
  assert.equal(contractAddress.toLowerCase(), logs[0].address)
  assert.lengthOf(logs[0].topics, 4)
  assert.equal(
    '35',
    web3.eth.abi.decodeParameter("int256", logs[0].data)
  )

  // make contract function call that emits another log
  res = await helpers.signAndSend({
    from: conf.eoa.address,
    to: contractAddress,
    data: deployed.contract.methods.sum(30, 20).encodeABI(),
    gas: 1000000,
    gasPrice: 0
  })

  assert.equal(res.receipt.status, conf.successStatus)

  // check the matching items from the logs filter include both logs
  // from the above 2 contract function calls
  response = await helpers.callRPCMethod('eth_getFilterLogs', [filterID])
  assert.equal(200, response.status)
  assert.isUndefined(response.body.error)

  logs = response.body.result
  assert.isDefined(logs)
  assert.equal(2, logs.length)
  assert.equal(contractAddress.toLowerCase(), logs[1].address)
  assert.lengthOf(logs[1].topics, 4)
  assert.equal(
    '50',
    web3.eth.abi.decodeParameter("int256", logs[1].data)
  )

})
