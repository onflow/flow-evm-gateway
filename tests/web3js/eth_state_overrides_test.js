const { assert } = require('chai')
const conf = require('./config')
const helpers = require('./helpers')
const web3 = conf.web3

it('should apply state overrides on previously deployed contract', async () => {
    let deployed = await helpers.deployContract('storage')
    let contractAddress = deployed.receipt.contractAddress
    let callRetrieve = deployed.contract.methods.retrieve().encodeABI()
    let callGetDeployer = deployed.contract.methods.getDeployer().encodeABI()

    const initValue = 1337
    const raw = await web3.eth.call({ to: contractAddress, data: callRetrieve }, 'latest')
    const decoded = web3.eth.abi.decodeParameter('uint256', raw)
    assert.strictEqual(Number(decoded), initValue)

    // check that `eth_call` can handle `stateDiff` state overrides
    // override only the first storage slot
    let stateOverrides = {
        [contractAddress]: {
            stateDiff: {
                '0x0000000000000000000000000000000000000000000000000000000000000000': '0x00000000000000000000000000000000000000000000000000000000000003e8'
            }
        }
    }
    let response = await helpers.callRPCMethod(
        'eth_call',
        [{ to: contractAddress, data: callRetrieve }, 'latest', stateOverrides]
    )
    assert.equal(response.status, 200)

    assert.equal(
        response.body.result,
        '0x00000000000000000000000000000000000000000000000000000000000003e8'
    )

    response = await helpers.callRPCMethod(
        'eth_call',
        [{ to: contractAddress, data: callGetDeployer }, 'latest', stateOverrides]
    )
    assert.equal(response.status, 200)

    // `stateDiff` should not alter any other storage slots, so here we should
    // get no changes.
    assert.equal(
        response.body.result,
        '0x000000000000000000000000ea02f564664a477286b93712829180be4764fae2'
    )

    // override multiple storage slots at once
    stateOverrides = {
        [contractAddress]: {
            stateDiff: {
                '0x0000000000000000000000000000000000000000000000000000000000000000': '0x00000000000000000000000000000000000000000000000000000000000013ba',
                '0x0000000000000000000000000000000000000000000000000000000000000001': '0x000000000000000000000000F376A6849184571fEEdD246a1Ba2D331cfe56c8c'
            }
        }
    }

    response = await helpers.callRPCMethod(
        'eth_call',
        [{ to: contractAddress, data: callRetrieve }, 'latest', stateOverrides]
    )
    assert.equal(response.status, 200)

    assert.equal(
        response.body.result,
        '0x00000000000000000000000000000000000000000000000000000000000013ba'
    )

    response = await helpers.callRPCMethod(
        'eth_call',
        [{ to: contractAddress, data: callGetDeployer }, 'latest', stateOverrides]
    )
    assert.equal(response.status, 200)

    assert.equal(
        response.body.result,
        '0x000000000000000000000000f376a6849184571feedd246a1ba2d331cfe56c8c'
    )

    // check that `eth_call` can handle `state` state overrides
    // override only the first storage slot
    stateOverrides = {
        [contractAddress]: {
            state: {
                '0x0000000000000000000000000000000000000000000000000000000000000000': '0x00000000000000000000000000000000000000000000000000000000000003e8'
            }
        }
    }
    response = await helpers.callRPCMethod(
        'eth_call',
        [{ to: contractAddress, data: callRetrieve }, 'latest', stateOverrides]
    )
    assert.equal(response.status, 200)

    assert.equal(
        response.body.result,
        '0x00000000000000000000000000000000000000000000000000000000000003e8'
    )

    response = await helpers.callRPCMethod(
        'eth_call',
        [{ to: contractAddress, data: callGetDeployer }, 'latest', stateOverrides]
    )
    assert.equal(response.status, 200)

    // `state` will purge all other storage slots, and only keep the storage slots given,
    // so here we get a zero value.
    assert.equal(
        response.body.result,
        '0x0000000000000000000000000000000000000000000000000000000000000000'
    )

    // override multiple storage slots at once
    stateOverrides = {
        [contractAddress]: {
            state: {
                '0x0000000000000000000000000000000000000000000000000000000000000000': '0x00000000000000000000000000000000000000000000000000000000000013ba',
                '0x0000000000000000000000000000000000000000000000000000000000000001': '0x000000000000000000000000F376A6849184571fEEdD246a1Ba2D331cfe56c8c'
            }
        }
    }

    response = await helpers.callRPCMethod(
        'eth_call',
        [{ to: contractAddress, data: callRetrieve }, 'latest', stateOverrides]
    )
    assert.equal(response.status, 200)

    assert.equal(
        response.body.result,
        '0x00000000000000000000000000000000000000000000000000000000000013ba'
    )

    response = await helpers.callRPCMethod(
        'eth_call',
        [{ to: contractAddress, data: callGetDeployer }, 'latest', stateOverrides]
    )
    assert.equal(response.status, 200)

    assert.equal(
        response.body.result,
        '0x000000000000000000000000f376a6849184571feedd246a1ba2d331cfe56c8c'
    )

    let eoaAddr = '0x7ce7964313547FaE08eBC035c7bA1A58500B3932'
    // check that `eth_call` can handle `balance` state overrides
    stateOverrides = {
        [eoaAddr]: {
            balance: '0xD02AB486CEDC0000'
        }
    }
    let callGetBalance = deployed.contract.methods.getBalance(eoaAddr).encodeABI()
    response = await helpers.callRPCMethod(
        'eth_call',
        [{ to: contractAddress, data: callGetBalance }, 'latest', stateOverrides]
    )
    assert.equal(response.status, 200)

    assert.equal(
        response.body.result,
        '0x000000000000000000000000000000000000000000000000d02ab486cedc0000'
    )

    stateOverrides = {
        [contractAddress]: {
            balance: '0xD02AB486CEDC0000'
        }
    }
    callGetBalance = deployed.contract.methods.getBalance(contractAddress).encodeABI()
    response = await helpers.callRPCMethod(
        'eth_call',
        [{ to: contractAddress, data: callGetBalance }, 'latest', stateOverrides]
    )
    assert.equal(response.status, 200)

    assert.equal(
        response.body.result,
        '0x000000000000000000000000000000000000000000000000d02ab486cedc0000'
    )
})

it('should apply state overrides on non-existent contract', async () => {
    let deployed = await helpers.deployContract('storage')
    let code = await web3.eth.getCode(deployed.receipt.contractAddress)
    let contractAddress = '0x7ce7964313547FaE08eBC035c7bA1A58500B3932'
    let callRetrieve = deployed.contract.methods.retrieve().encodeABI()
    let callGetDeployer = deployed.contract.methods.getDeployer().encodeABI()

    const initValue = 1337
    const raw = await web3.eth.call({ to: deployed.receipt.contractAddress, data: callRetrieve }, 'latest')
    const decoded = web3.eth.abi.decodeParameter('uint256', raw)
    assert.strictEqual(Number(decoded), initValue)

    // check that `eth_call` can handle `stateDiff` state overrides
    // override only the first storage slot
    let stateOverrides = {
        [contractAddress]: {
            code: code,
            stateDiff: {
                '0x0000000000000000000000000000000000000000000000000000000000000000': '0x00000000000000000000000000000000000000000000000000000000000003e8'
            }
        }
    }
    let response = await helpers.callRPCMethod(
        'eth_call',
        [{ to: contractAddress, data: callRetrieve }, 'latest', stateOverrides]
    )
    assert.equal(response.status, 200)
    assert.equal(
        response.body.result,
        '0x00000000000000000000000000000000000000000000000000000000000003e8'
    )

    // override multiple storage slots at once
    stateOverrides = {
        [contractAddress]: {
            code: code,
            stateDiff: {
                '0x0000000000000000000000000000000000000000000000000000000000000000': '0x00000000000000000000000000000000000000000000000000000000000013ba',
                '0x0000000000000000000000000000000000000000000000000000000000000001': '0x000000000000000000000000F376A6849184571fEEdD246a1Ba2D331cfe56c8c'
            }
        }
    }

    response = await helpers.callRPCMethod(
        'eth_call',
        [{ to: contractAddress, data: callRetrieve }, 'latest', stateOverrides]
    )
    assert.equal(response.status, 200)

    assert.equal(
        response.body.result,
        '0x00000000000000000000000000000000000000000000000000000000000013ba'
    )

    response = await helpers.callRPCMethod(
        'eth_call',
        [{ to: contractAddress, data: callGetDeployer }, 'latest', stateOverrides]
    )
    assert.equal(response.status, 200)

    assert.equal(
        response.body.result,
        '0x000000000000000000000000f376a6849184571feedd246a1ba2d331cfe56c8c'
    )

    // check that `eth_call` can handle `state` state overrides
    // override only the first storage slot
    stateOverrides = {
        [contractAddress]: {
            code: code,
            state: {
                '0x0000000000000000000000000000000000000000000000000000000000000000': '0x00000000000000000000000000000000000000000000000000000000000003e8'
            }
        }
    }
    response = await helpers.callRPCMethod(
        'eth_call',
        [{ to: contractAddress, data: callRetrieve }, 'latest', stateOverrides]
    )
    assert.equal(response.status, 200)

    assert.equal(
        response.body.result,
        '0x00000000000000000000000000000000000000000000000000000000000003e8'
    )

    response = await helpers.callRPCMethod(
        'eth_call',
        [{ to: contractAddress, data: callGetDeployer }, 'latest', stateOverrides]
    )
    assert.equal(response.status, 200)

    // `state` will purge all other storage slots, and only keep the storage slots given,
    // so here we get a zero value.
    assert.equal(
        response.body.result,
        '0x0000000000000000000000000000000000000000000000000000000000000000'
    )

    // override multiple storage slots at once
    stateOverrides = {
        [contractAddress]: {
            code: code,
            state: {
                '0x0000000000000000000000000000000000000000000000000000000000000000': '0x00000000000000000000000000000000000000000000000000000000000013ba',
                '0x0000000000000000000000000000000000000000000000000000000000000001': '0x000000000000000000000000F376A6849184571fEEdD246a1Ba2D331cfe56c8c'
            }
        }
    }

    response = await helpers.callRPCMethod(
        'eth_call',
        [{ to: contractAddress, data: callRetrieve }, 'latest', stateOverrides]
    )
    assert.equal(response.status, 200)

    assert.equal(
        response.body.result,
        '0x00000000000000000000000000000000000000000000000000000000000013ba'
    )

    response = await helpers.callRPCMethod(
        'eth_call',
        [{ to: contractAddress, data: callGetDeployer }, 'latest', stateOverrides]
    )
    assert.equal(response.status, 200)

    assert.equal(
        response.body.result,
        '0x000000000000000000000000f376a6849184571feedd246a1ba2d331cfe56c8c'
    )

    stateOverrides = {
        [contractAddress]: {
            code: code,
            balance: '0xD02AB486CEDC0000'
        }
    }
    let callGetBalance = deployed.contract.methods.getBalance(contractAddress).encodeABI()
    response = await helpers.callRPCMethod(
        'eth_call',
        [{ to: contractAddress, data: callGetBalance }, 'latest', stateOverrides]
    )
    assert.equal(response.status, 200)

    assert.equal(
        response.body.result,
        '0x000000000000000000000000000000000000000000000000d02ab486cedc0000'
    )
})
