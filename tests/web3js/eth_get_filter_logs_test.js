const chai = require('chai')
const chaiHttp = require('chai-http')
const conf = require('./config')
const helpers = require('./helpers')
const web3 = conf.web3
chai.use(chaiHttp);

it('returns a null result for missing filter', async () => {
  // check that null is returned when the filter could not be found.
  let response = await callRPCMethod('eth_getFilterLogs', ['0xffa1'])

  chai.assert.equal(200, response.status)
  chai.assert.isUndefined(response.body['result'])
})

it('create logs filter and call eth_getFilterLogs', async () => {
  // deploy contract
  let deployed = await helpers.deployContract('storage')
  let contractAddress = deployed.receipt.contractAddress

  // create logs filter on the address of the deployed contract
  let response = await callRPCMethod('eth_newFilter', [{ 'address': contractAddress }])

  chai.assert.equal(200, response.status)
  chai.assert.isDefined(response.body['result'])
  let filterID = response.body['result']

  // make contract function call that emits a log
  let res = await helpers.signAndSend({
    from: conf.eoa.address,
    to: contractAddress,
    data: deployed.contract.methods.sum(15, 20).encodeABI(),
    gas: 1000000,
    gasPrice: 0
  })
  chai.assert.equal(res.receipt.status, conf.successStatus)

  // check the matching items from the logs filter
  response = await callRPCMethod('eth_getFilterLogs', [filterID])

  chai.assert.equal(200, response.status)
  chai.assert.equal(1, response.body['result'].length)

  let logs = response.body['result']
  chai.assert.equal(contractAddress.toLowerCase(), logs[0].address)
  chai.assert.lengthOf(logs[0].topics, 4)
  chai.assert.equal(
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

  chai.assert.equal(res.receipt.status, conf.successStatus)

  // check the matching items from the logs filter include both logs
  // from the above 2 contract function calls
  response = await callRPCMethod('eth_getFilterLogs', [filterID])

  chai.assert.equal(200, response.status)
  chai.assert.equal(2, response.body['result'].length)
  logs = response.body['result']
  console.log()

  chai.assert.equal(contractAddress.toLowerCase(), logs[1].address)
  chai.assert.lengthOf(logs[1].topics, 4)
  chai.assert.equal(
    '50',
    web3.eth.abi.decodeParameter("int256", logs[1].data)
  )

}).timeout(10 * 1000)

async function callRPCMethod(methodName, params) {
  return chai.request('http://127.0.0.1:8545')
    .post('/')
    .set('Content-Type', 'application/json')
    .set('Accept', 'application/json')
    .send({
      'jsonrpc': '2.0',
      'method': methodName,
      'id': 1,
      'params': params
    })
}
