// SPDX-License-Identifier: GPL-3.0

pragma solidity >=0.8.2 <0.9.0;

contract Storage {
    address constant public cadenceArch = 0x0000000000000000000000010000000000000001;
    event NewStore(address indexed caller, uint256 indexed value);
    event Calculated(address indexed caller, int indexed numA, int indexed numB, int sum);
    event Retrieved(address indexed caller, uint256 value);
    event BlockNumber(address indexed caller, uint256 value);
    event BlockTime(address indexed caller, uint value);
    event BlockHash(address indexed caller, bytes32 hash);
    event Random(address indexed caller, uint256 value);
    event ChainID(address indexed caller, uint256 value);
    event VerifyArchCallToRandomSource(address indexed caller, bytes32 output);
    event VerifyArchCallToRevertibleRandom(address indexed caller, uint64 output);
    event VerifyArchCallToFlowBlockHeight(address indexed caller, uint64 output);
    event VerifyArchCallToVerifyCOAOwnershipProof(address indexed caller, bool output);

    error MyCustomError(uint value, string message);

    uint256 number;

    constructor() payable {
        number = 1337;
    }

    function store(uint256 num) public {
        number = num;
    }

    function storeWithLog(uint256 num) public {
        emit NewStore(msg.sender, num);
        number = num;
    }

    function storeButRevert(uint256 num) public {
        number = num;
        revert();
    }

    function retrieve() public view returns (uint256) {
        return number;
    }

    function emitRetrieve() public {
        emit Retrieved(msg.sender, number);
    }

    function sum(int A, int B) public returns (int) {
        int s = A + B;
        emit Calculated(msg.sender, A, B, s);
        return s;
    }

    function blockNumber() public view returns (uint256) {
        return block.number;
    }

    function emitBlockNumber() public {
        emit BlockNumber(msg.sender, block.number);
    }

    function blockTime() public view returns (uint) {
        return block.timestamp;
    }

    function emitBlockTime() public {
        emit BlockTime(msg.sender, block.timestamp);
    }

    function blockHash(uint num) public view returns (bytes32) {
        return blockhash(num);
    }

    function emitBlockHash(uint num) public {
        emit BlockHash(msg.sender, blockhash(num));
    }

    function random() public view returns (uint256) {
        return block.prevrandao;
    }

    function emitRandom() public {
        emit Random(msg.sender, block.prevrandao);
    }

    function chainID() public view returns (uint256) {
        return block.chainid;
    }

    function emitChainID() public {
        emit ChainID(msg.sender, block.chainid);
    }

    function destroy() public {
        selfdestruct(payable(msg.sender));
    }

    function assertError() public pure {
        require(false, "Assert Error Message");
    }

    function customError() public pure {
        revert MyCustomError(5, "Value is too low");
    }

    function verifyArchCallToRandomSource(uint64 height) public view returns (bytes32) {
        (bool ok, bytes memory data) = cadenceArch.staticcall(abi.encodeWithSignature("getRandomSource(uint64)", height));
        require(ok, "unsuccessful call to arch ");
        bytes32 output = abi.decode(data, (bytes32));
        return output;
    }

    function emitVerifyArchCallToRandomSource(uint64 height) public {
        bytes32 output = verifyArchCallToRandomSource(height);
        emit VerifyArchCallToRandomSource(msg.sender, output);
    }

    function verifyArchCallToRevertibleRandom() public view returns (uint64) {
        (bool ok, bytes memory data) = cadenceArch.staticcall(abi.encodeWithSignature("revertibleRandom()"));
        require(ok, "unsuccessful call to arch");
        uint64 output = abi.decode(data, (uint64));
        return output;
    }

    function emitVerifyArchCallToRevertibleRandom() public {
        uint64 output = verifyArchCallToRevertibleRandom();
        emit VerifyArchCallToRevertibleRandom(msg.sender, output);
    }

    function verifyArchCallToFlowBlockHeight() public view returns (uint64) {
        (bool ok, bytes memory data) = cadenceArch.staticcall(abi.encodeWithSignature("flowBlockHeight()"));
        require(ok, "unsuccessful call to arch ");
        uint64 output = abi.decode(data, (uint64));
        return output;
    }

    function emitVerifyArchCallToFlowBlockHeight() public {
        uint64 output = verifyArchCallToFlowBlockHeight();
        emit VerifyArchCallToFlowBlockHeight(msg.sender, output);
    }

    function verifyArchCallToVerifyCOAOwnershipProof(address arg0, bytes32 arg1, bytes memory arg2) public view returns (bool) {
        (bool ok, bytes memory data) = cadenceArch.staticcall(abi.encodeWithSignature("verifyCOAOwnershipProof(address,bytes32,bytes)", arg0, arg1, arg2));
        require(ok, "unsuccessful call to arch");
        bool output = abi.decode(data, (bool));
        return output;
    }

    function emitVerifyArchCallToVerifyCOAOwnershipProof(address arg0, bytes32 arg1, bytes memory arg2) public {
        bool output = verifyArchCallToVerifyCOAOwnershipProof(arg0, arg1, arg2);
        emit VerifyArchCallToVerifyCOAOwnershipProof(msg.sender, output);
    }
}