package resizeengine

import "hash/fnv"

// hash generates a hash value for a given string using the FNV-1a algorithm.
// s: The input string to be hashed.
// returns: A uint32 hash value that represents the input string.
func hash(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}

// getShardIndex calculates the shard index for a given key and number of shards.
// key: The input string for which the shard index is to be calculated.
// numShards: The total number of shards available.
// returns: An integer representing the shard index for the given key.
func GetShardIndex(key string, numShards int) int {
	return int(hash(key)) % numShards
}
