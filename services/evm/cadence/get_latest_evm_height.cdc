import EVM

access(all)
fun main(): UInt64 {
    return EVM.getLatestBlock().height
}
