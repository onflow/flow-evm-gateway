const { assert } = require('chai')
const conf = require('./config')
const helpers = require('./helpers')
const web3 = conf.web3

it('emit logs and retrieve them using different filters', async() => {
    let deployed = await helpers.deployContract("storage")
    let contractAddress = deployed.receipt.contractAddress

    const testValues = [
        { A: 1, B: 2 },
        { A: -1, B: -2 },
        { A: 1000, B: 2000 },
    ];

    for (const { A, B } of testValues) {
        const txObject = {
            from: conf.eoa.address,
            to: contractAddress,
            data: deployed.contract.methods.sum(A, B).encodeABI(),
            gas: 1000000,
            gasPrice: 0
        }

        const signedTx = await conf.eoa.signTransaction(txObject)
        // send transaction and make sure interaction was success
        const receipt = await web3.eth.sendSignedTransaction(signedTx.rawTransaction)
        assert.equal(receipt, conf.successStatus)

        // Event filtering based on the method inputs
        const events = await deployed.contract.getPastEvents('Calculated', {
            filter: { numA: A, numB: B },
            fromBlock: conf.startBlockHeight,
            toBlock: 'latest',
        })

        // Assert that the event is found and the result is correct
        assert.equal(events.length, 1)
        assert.equal(events[0].returnValues.sum, (A + B).toString())
    }

})