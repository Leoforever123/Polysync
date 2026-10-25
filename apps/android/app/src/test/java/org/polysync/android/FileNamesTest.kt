package org.polysync.android
import org.junit.Assert.*
import org.junit.Test
class FileNamesTest {
    @Test fun importsCannotBecomePaths() { assertEquals(".._private_key", FileNames.safe("../private\\key")); assertEquals("报告.txt", FileNames.safe("报告.txt")) }
    @Test fun refusesEmptyAndOverlongNames() { for (name in listOf("", "..", "中".repeat(100))) { try { FileNames.safe(name); fail("Accepted invalid name") } catch (_: IllegalArgumentException) {} } }
}
