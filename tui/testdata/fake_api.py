#!/usr/bin/env python3
"""假后端：把 QQMUSIC_API 指向这个文件，测试就完全不碰网络。

它模仿的是 qqmusic_api.py 的对外行为，而不是它的内部实现：
- 成功：stdout 一行 JSON 信封 {"ok":true,...}，exit 0
- 业务失败：stdout 一行 {"ok":false,"error":"中文"}，exit 1
- 参数错：stderr 打 usage，stdout 为空，exit 2（argparse 的行为）

测试用的开关（都是环境变量）：
  FAKE_ERROR=<msg>   返回业务错误信封，exit 1
  FAKE_EXIT=<code>   模拟 argparse 失败（只在 code=2 时有意义）
  FAKE_CRASH=1       模拟脚本自身崩溃：stderr 打 traceback，stdout 为空，exit 1
  FAKE_SLEEP=<秒>    先睡再答复，用来触发超时
  FAKE_LOGIN_STATE   让 login-poll 返回指定状态（waiting/scanned/success/expired/error）
  FAKE_QR_PATH       让 login-qr 回显指定的二维码文件路径
  FAKE_AUDIO         resolve 回这个地址（测试用本地音频，mpv 才真能放）
  FAKE_MAX_QUALITY   resolve 能拿到的最高音质，所选更高时降到它（模拟「这首没有无损」）
  FAKE_PL_FILE       「服务端」歌单状态文件：library 的 createdPlaylists 读它，
                     playlist-create / playlist-delete / playlist-song 改它
  FAKE_PRAISE_ERROR  让 comment-praise 返回这条业务错误
  FAKE_COMMENT_PENDING=1  让 comment-add 回「已受理、待审核」
  FAKE_FAV_FILE      「服务端」收藏状态存放的文件（每行一个 songMid）：
                     favorite 增删它、favorite-check 读它。不设时 favorite-check 返回业务错误，
                     用来覆盖「查不到收藏状态就退回本地判断」的兜底路径
"""

import json
import os
import sys
import time

FAKE_TRACK = {
    "id": "1001",
    "songMid": "0039MnYb0qxYhV",
    "mediaMid": "0039MnYb0qxYhV",
    "title": "假歌一号",
    "artists": ["假歌手"],
    "artist": "假歌手",
    "album": "假专辑",
    "albumMid": "000MkMni19ClKG",
    "duration": 214,
    "coverUrl": "https://example.invalid/cover.jpg",
    "vip": False,
    "playable": True,
}

FAKE_PLAYLIST = {
    "id": "9001",
    "kind": "playlist",
    "dirId": "0",
    "title": "假歌单",
    "subtitle": "测试用",
    "coverUrl": "https://example.invalid/pl.jpg",
    "songCount": 1,
    "playCount": 42,
    "creator": "假用户",
}

FAKE_ALBUM = {
    **FAKE_PLAYLIST,
    "id": "000MkMni19ClKG",
    "kind": "album",
    "title": "假专辑",
    "creator": "假歌手",
}

FAKE_SINGER = {
    "id": "2001",
    "mid": "0025NhlN2yWrP4",
    "name": "假歌手",
    "avatarUrl": "https://example.invalid/avatar.jpg",
    "songCount": 12,
    "albumCount": 3,
    "mvCount": 1,
}

# 分页用：一页 30 条、总共 45 条，这样第二页既非空又能收尾。
FAKE_TOTAL = 45
FAKE_PAGE_SIZE = 30


def emit(payload, code=0):
    sys.stdout.write(json.dumps(payload, ensure_ascii=False, separators=(",", ":")) + "\n")
    sys.exit(code)


def main():
    delay = os.environ.get("FAKE_SLEEP", "").strip()
    if delay:
        time.sleep(float(delay))

    if os.environ.get("FAKE_CRASH"):
        sys.stderr.write(
            "Traceback (most recent call last):\n"
            '  File "qqmusic_api.py", line 1, in <module>\n'
            "RuntimeError: 模拟的脚本崩溃\n"
        )
        sys.exit(1)

    force_exit = os.environ.get("FAKE_EXIT", "").strip()
    if force_exit:
        code = int(force_exit)
        sys.stderr.write(
            "usage: qqmusic_api.py [-h] {search,resolve,...} ...\n"
            "qqmusic_api.py: error: the following arguments are required: query\n"
        )
        sys.exit(code)

    forced_error = os.environ.get("FAKE_ERROR", "").strip()
    if forced_error:
        emit({"ok": False, "error": forced_error}, 1)

    argv = sys.argv[1:]
    cmd = argv[0] if argv else ""

    def flag(name, default=""):
        if name in argv:
            i = argv.index(name)
            if i + 1 < len(argv):
                return argv[i + 1]
        return default

    if cmd == "search":
        query = argv[1] if len(argv) > 1 else ""
        kind = flag("--type", "song")
        try:
            page = int(flag("--page", "1"))
        except ValueError:
            page = 1

        payload = {
            "ok": True, "query": query, "type": kind, "page": page,
            "tracks": [], "singers": [], "albums": [], "playlists": [],
            "total": 1, "hasMore": False,
        }
        if kind == "singer":
            payload["singers"] = [FAKE_SINGER]
        elif kind == "album":
            payload["albums"] = [FAKE_ALBUM]
        elif kind == "songlist":
            payload["playlists"] = [FAKE_PLAYLIST]
        elif os.environ.get("FAKE_PAGED"):
            # 分页场景：一页 30 条、共 45 条。曲名带序号，测试靠它确认
            # 第二页是「追加」而不是「替换」。
            start = (page - 1) * FAKE_PAGE_SIZE
            payload["tracks"] = [
                {**FAKE_TRACK, "id": "1001-%d" % (i + 1),
                 "songMid": "mid-%d" % (i + 1),
                 "title": "%s 第 %d 首" % (FAKE_TRACK["title"], i + 1)}
                for i in range(start, min(start + FAKE_PAGE_SIZE, FAKE_TOTAL))
            ]
            payload["total"] = FAKE_TOTAL
            payload["hasMore"] = start + len(payload["tracks"]) < FAKE_TOTAL
        else:
            payload["tracks"] = [FAKE_TRACK]
        emit(payload)

    if cmd == "album":
        emit({"ok": True, "playlist": {**FAKE_ALBUM, "id": flag("--album-mid")},
              "page": int(flag("--page", "1")), "total": 1,
              "tracks": [FAKE_TRACK], "hasMore": False})

    if cmd == "comments":
        song_id = flag("--song-id")
        try:
            page = int(flag("--page", "1"))
        except ValueError:
            page = 1
        sort = flag("--sort", "hot")
        # avatarUrl 回显收到的游标：测试靠它确认「往后翻用上一页末条 seqNo、往回翻复用旧游标」。
        comments = [
            {"id": "c%d" % (page * 100 + n), "seqNo": str(page * 100 + n), "author": "听众 %d" % n,
             "avatarUrl": "cursor:" + flag("--cursor"),
             "text": ("最新 " if sort == "new" else "") + "第 %d 页的第 %d 条评论" % (page, n),
             "time": 1700000000 + n, "likes": n, "replies": 0, "isPraised": n == 2}
            for n in range(1, 4)
        ]
        emit({"ok": True, "songId": song_id, "sort": sort, "page": page, "total": 7,
              "hasMore": page < 3, "comments": comments})

    if cmd == "comment-praise":
        if os.environ.get("FAKE_PRAISE_ERROR"):
            emit({"ok": False, "error": os.environ["FAKE_PRAISE_ERROR"]}, code=1)
        emit({"ok": True, "commentId": flag("--comment-id"), "praised": argv[1] == "like"})

    if cmd == "comment-add":
        emit({"ok": True, "pending": os.environ.get("FAKE_COMMENT_PENDING") == "1", "comment": {}})

    if cmd == "resolve":
        song_mid = flag("--song-mid")
        payload = {"ok": True, "songMid": song_mid, "mediaMid": flag("--media-mid") or song_mid,
                   "url": os.environ.get("FAKE_AUDIO") or "https://example.invalid/audio.m4a"}
        quality = flag("--quality")
        if quality:
            ladder = ["std", "128", "320", "flac"]
            top = os.environ.get("FAKE_MAX_QUALITY") or "flac"
            got = quality if ladder.index(quality) <= ladder.index(top) else top
            payload["quality"] = got
        emit(payload)

    if cmd == "lyrics":
        emit({"ok": True, "songMid": flag("--song-mid"), "lyrics": [
            {"time": 0.0, "text": "第一句", "translation": ""},
            {"time": 3.5, "text": "第二句", "translation": "Line two"},
        ]})

    if cmd == "session-status":
        emit({"ok": True, "loggedIn": True, "uin": "1402321235",
              "nickname": "假用户", "avatarUrl": "", "accountType": "qq",
              "sessionState": "active"})

    if cmd == "login-qr":
        emit({"ok": True, "type": flag("--type", "qq"), "state": "waiting",
              "message": "等待 QQ 扫码",
              "qrPath": os.environ.get("FAKE_QR_PATH") or "/tmp/fake-login-qr.png"})

    if cmd == "login-poll":
        # 脚本里 --wait 是死参数，这里也不理会它。
        state = os.environ.get("FAKE_LOGIN_STATE", "").strip() or "waiting"
        payload = {"ok": True, "state": state, "message": "等待 QQ 扫码", "type": "qq"}
        if state == "scanned":
            payload["message"] = "已扫码，请在手机上确认"
        elif state == "success":
            payload.update({"loggedIn": True, "uin": "1402321235",
                            "nickname": "假用户", "avatarUrl": "", "accountType": "qq"})
        elif state == "expired":
            payload["message"] = "二维码已过期"
        elif state == "error":
            payload["message"] = "登录失败，请重试"
        emit(payload)

    if cmd == "logout":
        emit({"ok": True, "loggedIn": False})

    if cmd == "discover":
        emit({"ok": True, "dailyPlaylist": FAKE_PLAYLIST, "dailyTracks": [FAKE_TRACK],
              "dailyRequiresLogin": False, "recommendedPlaylists": [FAKE_PLAYLIST],
              "guessTracks": [FAKE_TRACK], "radarTracks": [], "favoriteSeedTracks": []})

    if cmd == "daily":
        emit({"ok": True, "playlist": FAKE_PLAYLIST, "tracks": [FAKE_TRACK]})

    pl_file = os.environ.get("FAKE_PL_FILE", "")

    def pl_state():
        if os.path.exists(pl_file):
            with open(pl_file) as f:
                return json.load(f)
        return {"created": [
            {**FAKE_PLAYLIST, "id": "1001", "dirId": "201", "title": "我喜欢"},
            {**FAKE_PLAYLIST, "id": "1002", "dirId": "5", "title": "自建一号"},
        ], "songs": {}, "next": 6}

    def pl_save(state):
        with open(pl_file, "w") as f:
            json.dump(state, f, ensure_ascii=False)

    if cmd == "library":
        created = pl_state()["created"] if pl_file else [FAKE_PLAYLIST]
        emit({"ok": True, "recentTracks": [FAKE_TRACK],
              "collectedPlaylists": [FAKE_PLAYLIST], "createdPlaylists": created,
              "requiresLogin": False, "sessionState": "active"})

    if cmd == "playlist-create":
        state = pl_state()
        name = flag("--name")
        if any(p["title"] == name for p in state["created"]):
            emit({"ok": False, "error": "已经有同名的歌单了，换个名字"}, code=1)
        pl = {**FAKE_PLAYLIST, "id": str(2000 + state["next"]), "dirId": str(state["next"]),
              "title": name, "songCount": 0}
        state["next"] += 1
        state["created"].append(pl)
        pl_save(state)
        emit({"ok": True, "playlist": pl})

    if cmd == "playlist-delete":
        state = pl_state()
        dir_id = flag("--dir-id")
        if dir_id == "201":
            emit({"ok": False, "error": "「我喜欢」不能删除"}, code=1)
        state["created"] = [p for p in state["created"] if p["dirId"] != dir_id]
        pl_save(state)
        emit({"ok": True, "dirId": dir_id})

    if cmd == "playlist-song":
        state = pl_state()
        action, dir_id, mid = argv[1], flag("--dir-id"), flag("--song-mid")
        songs = state["songs"].setdefault(dir_id, [])
        if action == "add" and mid not in songs:
            songs.append(mid)
        elif action == "remove" and mid in songs:
            songs.remove(mid)
        for p in state["created"]:
            if p["dirId"] == dir_id:
                p["songCount"] = len(songs)
        pl_save(state)
        emit({"ok": True, "dirId": int(dir_id), "songMid": mid, "added": action == "add"})

    if cmd == "favorites":
        emit({"ok": True, "page": 1, "total": 1, "mids": [FAKE_TRACK["songMid"]],
              "tracks": [FAKE_TRACK], "hasMore": False})

    if cmd == "favorite":
        # argv 形如：favorite <add|remove> --song-mid X [--song-id N]
        action = argv[1] if len(argv) > 1 else ""
        if action not in ("add", "remove"):
            sys.stderr.write("fake_api.py: error: 未知的收藏操作 %r\n" % action)
            sys.exit(2)
        fav_file = os.environ.get("FAKE_FAV_FILE", "")
        if fav_file:
            mids = set(open(fav_file).read().split()) if os.path.exists(fav_file) else set()
            if action == "add":
                mids.add(flag("--song-mid"))
            else:
                mids.discard(flag("--song-mid"))
            with open(fav_file, "w") as f:
                f.write("\n".join(sorted(mids)))
        # songId 跟真后端一样回数字：脚本里它是 int(song_id) 出来的，不是字符串。
        raw_id = flag("--song-id")
        emit({"ok": True, "songMid": flag("--song-mid"),
              "songId": int(raw_id) if raw_id.isdigit() else 0,
              "favorite": action == "add"})

    if cmd == "favorite-check":
        fav_file = os.environ.get("FAKE_FAV_FILE", "")
        if not fav_file:
            emit({"ok": False, "error": "假后端未开启收藏状态"}, code=1)
        mids = set(open(fav_file).read().split()) if os.path.exists(fav_file) else set()
        mid = flag("--song-mid")
        emit({"ok": True, "fans": {mid: mid in mids}})

    if cmd == "playlist":
        # 回显 id：测试靠它判断歌单响应会不会串台。
        emit({"ok": True, "playlist": {**FAKE_PLAYLIST, "id": flag("--id")},
              "page": 1, "total": 1, "tracks": [FAKE_TRACK], "hasMore": False})
    # 没实现的子命令：按「参数错」处理，让测试立刻发现假后端没跟上。
    sys.stderr.write("fake_api.py: error: 未实现的子命令 %r\n" % cmd)
    sys.exit(2)


if __name__ == "__main__":
    main()
