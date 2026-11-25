package com.memora.app

import android.content.Context
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.longPreferencesKey
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map

// 这四样都不是业务数据：地址是配置，草稿是为了"别让用户白打一段字"（需求 §5 第 3 条），
// lastKind 只省一次点击。记录与报表正文一律不缓存，理由见 docs/design/android-client.md。
private val Context.dataStore: DataStore<Preferences> by preferencesDataStore(name = "memora_local")

data class Draft(val noteId: Long? = null, val body: String = "")

class LocalStore(context: Context) {

    private val store = context.dataStore

    // 没填过就用 flavor 的默认值；server flavor 的默认值是空串，于是连接页必须手填。
    val baseUrl: Flow<String> = store.data.map { it[KEY_URL] ?: BuildConfig.DEFAULT_BASE_URL }

    val draft: Flow<Draft> = store.data.map { prefs ->
        Draft(noteId = prefs[KEY_DRAFT_ID], body = prefs[KEY_DRAFT_BODY] ?: "")
    }

    val lastKind: Flow<String> = store.data.map { prefs ->
        if (prefs[KEY_KIND] == MONTH) MONTH else WEEK
    }

    suspend fun setBaseUrl(url: String) {
        store.edit { it[KEY_URL] = url.trim() }
    }

    suspend fun saveDraft(draft: Draft) {
        store.edit { prefs ->
            val id = draft.noteId
            if (id == null) prefs.remove(KEY_DRAFT_ID) else prefs[KEY_DRAFT_ID] = id
            prefs[KEY_DRAFT_BODY] = draft.body
        }
    }

    suspend fun clearDraft() {
        store.edit { prefs ->
            prefs.remove(KEY_DRAFT_ID)
            prefs.remove(KEY_DRAFT_BODY)
        }
    }

    suspend fun setLastKind(kind: String) {
        store.edit { it[KEY_KIND] = kind }
    }

    private companion object {
        val KEY_URL = stringPreferencesKey("base_url")
        val KEY_DRAFT_ID = longPreferencesKey("draft_note_id")
        val KEY_DRAFT_BODY = stringPreferencesKey("draft_body")
        val KEY_KIND = stringPreferencesKey("report_kind")

        const val WEEK = "week"
        const val MONTH = "month"
    }
}
