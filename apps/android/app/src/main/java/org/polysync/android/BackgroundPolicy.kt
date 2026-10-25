package org.polysync.android

/** Monotonic idle accounting includes queued native import/export operations. */
class BackgroundPolicy(private val now: () -> Long, private val idleMillis: Long = 10 * 60_000L) {
    private var lastActive = now()
    private var work = 0
    @Synchronized fun beginWork() { work++; lastActive = now() }
    @Synchronized fun endWork() { check(work > 0); work--; lastActive = now() }
    @Synchronized fun touch() { lastActive = now() }
    @Synchronized fun shouldStop(visible: Boolean, coreActive: Boolean): Boolean {
        if (visible || coreActive || work > 0) { lastActive = now(); return false }
        return now() - lastActive > idleMillis
    }
}
