# 市场与下载

市场模块是 Hugging Face 与 ModelScope 的集成入口：搜索、查看详情、浏览文件树、下载。

## 搜索与浏览

- 顶部切换来源（Hugging Face / ModelScope）与任务类型。
- 输入关键词搜索，点击卡片进入详情。
- 详情页展示模型说明、文件树（分页加载大仓库）、下载按钮。

## 下载

- **文件树勾选**：可只下载需要的文件（例如只下 `config.json` + 权重，跳过冗余的 `.bin`）。
- **队列与并发**：多个下载任务排队，后台并发执行（默认上限 3）。
- **断点续传**：下载中断后可恢复，不必重头开始。
- **自动重试**：网络波动时自动重试（指数退避，最多 3 次）。
- 在 **下载中心** 查看进度、暂停 / 恢复 / 取消。

## 配置 Token（提高下载配额 / 访问私有模型）

部分模型需要登录，下载频繁时也建议配置 token：

1. 前往对应平台获取凭证：
   - Hugging Face：https://huggingface.co/settings/tokens （粘贴 **Access Token**，形如 `hf_xxx`）
   - ModelScope：https://www.modelscope.cn/my/myaccesstoken （粘贴 **访问令牌**）
2. 在 **账户** 中填入 token，系统会自动后台验证。
3. 验证结果会显示用户名；失败时给出明确原因（网络错误 / 凭证被拒绝 / 过期等）。

> EverEvo 会自动识别你填的是 token 还是 cookie：token 以 `Authorization: Bearer` 发送，cookie 以 `Cookie` 发送，二者皆可。
