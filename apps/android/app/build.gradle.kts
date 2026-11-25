plugins { id("com.android.application"); id("org.jetbrains.kotlin.android") }
android {
    namespace = "org.polysync.android"
    compileSdk = 36
    useLibrary("android.test.runner")
    useLibrary("android.test.base")
    useLibrary("android.test.mock")
    defaultConfig {
        applicationId = "org.polysync.android.preview"
        minSdk = 29
        targetSdk = 36
        versionCode = 7
        versionName = "0.5.3-dev.7"
        ndk { abiFilters += providers.gradleProperty("polysyncAbi").orElse("arm64-v8a").get() }
        testInstrumentationRunner = "android.test.InstrumentationTestRunner"
    }
    compileOptions { sourceCompatibility = JavaVersion.VERSION_17; targetCompatibility = JavaVersion.VERSION_17 }
    kotlinOptions { jvmTarget = "17" }
    buildTypes { getByName("release") { isMinifyEnabled = false } }
}
dependencies { implementation(files("libs/polysync.aar")); testImplementation("junit:junit:4.13.2") }
