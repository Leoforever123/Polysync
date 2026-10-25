package org.polysync.android

object FileNames {
    fun safe(value: String): String {
        val result = value.replace('\\', '_').replace('/', '_').filter { it >= ' ' && it != '\u007f' }.trim()
        require(result.isNotBlank() && result != "." && result != ".." && result.toByteArray().size < 240) { "文件名称无效或过长" }
        return result
    }
}
