const helpers = require("./helpers");
const conf = require("./config");
const web3 = conf.web3

it('transfer failure due to incorrect nonce', async () => {
    let receiver = web3.eth.accounts.create()

    try {
        let transfer = await helpers.signAndSend({
            from: conf.eoa.address,
            to: receiver.address,
            value: 1,
            gasPrice: '0',
            gasLimit: 55000,
            nonce: 1337, // invalid
        })
    } catch (e) {
        console.log("ERROR", e)
    }
})