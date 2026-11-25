package com.memora.app

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

// 口令是整个访问控制的唯一防线（ADR-0002），所以明文只存在于内存：
// 落盘的是 KeyStore 密钥加密后的密文（ADR-0008 的连带决定），不是偏好文件里的原文。
class CredentialStore(context: Context) {

    private val prefs = context.getSharedPreferences("memora_credential", Context.MODE_PRIVATE)

    fun passphrase(): String? {
        return try {
            val stored = prefs.getString(STORED_KEY, null)
            if (stored == null) return null
            val separator = stored.indexOf(':')
            if (separator < 0) return null
            val iv = Base64.decode(stored.substring(0, separator), Base64.NO_WRAP)
            val cipherText = Base64.decode(stored.substring(separator + 1), Base64.NO_WRAP)

            val cipher = Cipher.getInstance(TRANSFORMATION)
            cipher.init(Cipher.DECRYPT_MODE, secretKey(), GCMParameterSpec(TAG_BITS, iv))
            String(cipher.doFinal(cipherText), Charsets.UTF_8)
        } catch (e: Exception) {
            // 密钥可能因为系统侧的原因再也用不了（恢复出厂外的 keystore 重置、别名被清）。
            // 密文留着也没有可读路径，清掉让界面回到连接页重输——比抛一个用户看不懂的错好。
            prefs.edit().remove(STORED_KEY).apply()
            null
        }
    }

    // 返回 false 表示这台设备存不住口令；调用方要告诉用户，而不是假装存好了。
    fun save(passphrase: String): Boolean = try {
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.ENCRYPT_MODE, secretKey())
        val cipherText = cipher.doFinal(passphrase.toByteArray(Charsets.UTF_8))
        val encoded = Base64.encodeToString(cipher.iv, Base64.NO_WRAP) + ":" +
            Base64.encodeToString(cipherText, Base64.NO_WRAP)
        prefs.edit().putString(STORED_KEY, encoded).apply()
        true
    } catch (e: Exception) {
        false
    }

    fun clear() {
        prefs.edit().remove(STORED_KEY).apply()
        runCatching {
            keyStore().deleteEntry(ALIAS)
        }
    }

    private fun secretKey(): SecretKey {
        val store = keyStore()
        (store.getKey(ALIAS, null) as? SecretKey)?.let { return it }

        val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, ANDROID_KEYSTORE)
        generator.init(
            KeyGenParameterSpec.Builder(ALIAS, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                // 不要求生物识别：现在只是防"拿到手机翻看偏好文件"，
                // 解锁强度问题按 ADR-0010 的复查条件再说。
                .setUserAuthenticationRequired(false)
                .build()
        )
        return generator.generateKey()
    }

    private fun keyStore(): KeyStore =
        KeyStore.getInstance(ANDROID_KEYSTORE).apply { load(null) }

    private companion object {
        const val ANDROID_KEYSTORE = "AndroidKeyStore"
        const val ALIAS = "memora-passphrase"
        const val TRANSFORMATION = "AES/GCM/NoPadding"
        const val TAG_BITS = 128
        const val STORED_KEY = "passphrase_v1"
    }
}
