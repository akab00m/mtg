// Package antireplay provides mechanisms to detect and prevent replay attacks.
//
// # Overview
//
// Replay attacks are a common attack vector where an attacker captures legitimate
// network traffic and replays it later to gain unauthorized access or cause other
// malicious effects. This package provides efficient data structures to detect
// duplicate messages in high-throughput scenarios.
//
// # Stable Bloom Filter
//
// The main implementation uses a Stable Bloom Filter, which is a probabilistic
// data structure that can maintain a constant false positive rate even with
// unbounded streams of data.
//
// Key characteristics:
//   - Constant memory usage (configurable via byteSize)
//   - Configurable false positive rate (default: 0.01 or 1%)
//   - Thread-safe via mutex protection
//   - No false negatives (all seen items are always detected)
//
// # Configuration
//
// Default values:
//   - Memory: 1 MB (DefaultStableBloomFilterMaxSize = 1024 * 1024 bytes)
//   - False positive rate: 1% (DefaultStableBloomFilterErrorRate = 0.01)
//
// These defaults provide good balance between memory usage and accuracy for
// most MTG proxy deployments. For higher traffic scenarios, increase byteSize.
//
// # Usage Example
//
//	cache := antireplay.NewStableBloomFilter(
//	    1024*1024,  // 1 MB memory
//	    0.01,       // 1% false positive rate
//	)
//
//	// Check if we've seen this digest before
//	if cache.SeenBefore(messageDigest) {
//	    // Potential replay attack - reject message
//	    return ErrReplayDetected
//	}
//
// # Performance Characteristics
//
// - Lookup time: O(k) where k is number of hash functions (typically 4-6)
// - Memory: Fixed at initialization (byteSize * 8 bits)
// - Thread-safety: Mutex-protected, may become bottleneck at >100k req/sec
//
// # Mathematical Foundation
//
// Based on "Approximately Detecting Duplicates for Streaming Data using Stable Bloom Filters"
// by Deng and Rafiei (2006): http://webdocs.cs.ualberta.ca/~drafiei/papers/DupDet06Sigmod.pdf
//
// The stability is achieved by randomly resetting P cells for each insertion,
// where P is chosen to maintain constant false positive rate as elements are added.
//
// # Security Considerations
//
//   - False positives (1% default): Legitimate messages may occasionally be rejected.
//     This is acceptable for DoS protection but adjust errorRate if critical.
//
//   - Hash collision attacks: Uses xxHash (non-cryptographic). For security-critical
//     applications, consider using HMAC-based hashing of input digests.
//
//   - Memory exhaustion: Fixed memory usage prevents unbounded growth, but ensure
//     byteSize is appropriate for your traffic volume.
//
// # Monitoring
//
// For production deployments, monitor:
//   - Number of rejected replays (should be low in normal operation)
//   - False positive rate (compare with expected 1%)
//   - Mutex contention (if performance degrades)
//
// See NewStableBloomFilterWithMetrics for instrumented version.
package antireplay
