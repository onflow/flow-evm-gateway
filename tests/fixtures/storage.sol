// SPDX-License-Identifier: GPL-3.0

pragma solidity >=0.8.2 <0.9.0;

contract Storage {

    address constant public cadenceArch = 0x0000000000000000000000010000000000000001;
    event NewStore(address indexed caller, uint256 indexed value);
    event Calculated(address indexed caller, int indexed numA, int indexed numB, int sum);

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

    function retrieve() public view returns (uint256){
        return number;
    }

    function sum(int A, int B) public returns (int) {
        int s = A+B;
        emit Calculated(msg.sender, A, B, s);
        return s;
    }

    function blockNumber() public view returns (uint256) {
        return block.number;
    }

    function blockTime() public view returns (uint) {
        return block.timestamp;
    }

    function blockHash(uint num)  public view returns (bytes32) {
        return blockhash(num);
    }

    function random() public view returns (uint256) {
        return block.prevrandao;
    }

    function chainID() public view returns (uint256) {
        return block.chainid;
    }

    function destroy() public {
        selfdestruct(payable(msg.sender));
    }

    function assertError() public pure{
        require(false, "Assert Error Message");
    }

    function customError() public pure{
       revert MyCustomError(5, "Value is too low");
    }

    function verifyArchCallToRandomSource(uint64 height) public view returns (bytes32) {
        (bool ok, bytes memory data) = cadenceArch.staticcall(abi.encodeWithSignature("getRandomSource(uint64)", height));
        require(ok, "unsuccessful call to arch ");
        bytes32 output = abi.decode(data, (bytes32));
        return output;
    }

    function verifyArchCallToRevertibleRandom() public view returns (uint64) {
        (bool ok, bytes memory data) = cadenceArch.staticcall(abi.encodeWithSignature("revertibleRandom()"));
        require(ok, "unsuccessful call to arch");
        uint64 output = abi.decode(data, (uint64));
        return output;
    }

    function verifyArchCallToFlowBlockHeight() public view returns (uint64){
        (bool ok, bytes memory data) = cadenceArch.staticcall(abi.encodeWithSignature("flowBlockHeight()"));
        require(ok, "unsuccessful call to arch ");
        uint64 output = abi.decode(data, (uint64));
        return output;
    }

    function verifyArchCallToVerifyCOAOwnershipProof(address arg0 , bytes32 arg1 , bytes memory arg2 ) public view returns (bool){
        (bool ok, bytes memory data) = cadenceArch.staticcall(abi.encodeWithSignature("verifyCOAOwnershipProof(address,bytes32,bytes)", arg0, arg1, arg2));
        require(ok, "unsuccessful call to arch");
        bool output = abi.decode(data, (bool));
        return output;
    }
}