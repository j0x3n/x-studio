# AI 模特库与试穿试戴

日期：2026-10-19。Claude：前端、提示词、接口、审核；Codex：后端、测试。沿用 clean 风格，代码落在 assets 模块。

## 1. 权限和边界

新增 `model.view`（看店铺模特）、`model.manage`（维护店铺模特），运营/美工默认两者，摄影默认查看；AI 操作还要 `asset.process`，工作台操作还要 `asset.upload`。外部 shares 用户不可访问店铺模特。模特必须属于有效且未归档店铺，`shop_id` 创建后不可改。

所有模特、参考、文件、生成、候选确认均检查权限和 `CanShop`。商品另外检查 `CanProduct`；商品共享不代表有权访问所有店铺模特。识别、生成、候选确认在执行和发布前重新加载用户权限/对象状态；请求不能传本机路径或远程 URL。

试穿和模特候选结果暂存在本人 workbench，7 天；永久参考放 `data/models/<model_id>/`，不被 workbench 清理。相关 workbench 结果读取/列表/文件/下载/保存需按 op 中关联 shop_id 重校 `model.view + CanShop`，撤权后返回404/过滤。归档模特阻止新生成和候选确认，但不影响有权限用户访问历史成果。清理/删除仍允许本人删除已不可见结果。存入商品素材后服从商品素材可见范围，前端说明此规则。

## 2. 数据和 JSON

`shop_models`：id、shop_id、name（必填≤80字）、description（≤4000字）、archived、cover_reference_id?、created_by、created_at、updated_at。

`model_references`：id、model_id、body_part、source（original/ai_generated）、file、orig_name、width、height、op JSON、created_by、created_at。不可替换字节；更换参考上传新图。无硬删除接口，本期通过归档模特停止使用。cover只能指向本档案的参考图。

`body_part` 枚举：`full_body/upper_body/hand/wrist/ear/neck/foot/custom`。参考图和试穿共用。`framing`：`full_body/half_body/closeup`。`aspect_ratio`：`1:1/2:3/3:2`，对应1024x1024/1024x1536/1536x1024。

Model JSON：`{id,shop_id,name,description,archived,cover_reference_id:null|number,cover_url:"",references:[],created_by,created_at,updated_at}`。
Reference JSON：`{id,model_id,body_part,source,orig_name,width,height,url,thumb_url,op,created_at}`。
模特列表 `{items,total}`，详情/创建/修改 `{model}`，上传/确认 `{reference,model}`。复用的 workbench 列表沿用 `{items}`，最多返回 200 张有权限的记录。

## 3. 接口

| 路径 | 请求 / 返回 |
|---|---|
| GET /api/models?shop_id=&q=&archived=0&page=&page_size= | 按店铺权限过滤；列表带 references；shop_id 可选 |
| POST /api/models | `{shop_id,name,description}` → 201 `{model}` |
| GET /api/models/{id} | `{model}` |
| PATCH /api/models/{id} | `{name?,description?,archived?,cover_reference_id?}` |
| POST /api/models/{id}/references | multipart `files`（最多1张）、`body_part`；复用真实格式/25MB/40M像素/EXIF处理 →201 `{reference,model}` |
| GET /api/models/{id}/references/{ref}/file?w=240\|480\|960 | 受保护图片，不暴露本机路径 |
| POST /api/models/{id}/generate | `{body_part,prompt,aspect_ratio,base_reference_id?}` →202 `{job}`；初次无基准则纯文生图，有基准则编辑；局部参考生成默认必须有基准（专用手模等首次创建可从文字生成） |
| POST /api/models/{id}/references/confirm | `{item_id,set_cover?:boolean}` →201 `{reference,model}`；候选必须属于本人、op.type=model_candidate、该model；事务确认幂等，op记录confirmed_reference_id；不覆盖已有基准 |
| POST /api/tryon/analyze | `{inputs:[{asset_id}|{input_id}],hint?:string}` →202 `{job}`；1–2张，同一次仅asset同商品或仅本人input，不混；支持不先识别直接手填 |
| POST /api/tryon/generate | `{inputs:[...],model_id,reference_id,body_part,framing,product_type,prompt,aspect_ratio}` →202 `{job}`；1–2商品图+1模特图，保留输入顺序；输入asset同商品或仅本人input，不混 |
| GET /api/tryon/state | 需asset.upload/process且非shares；`{job:null|Job}` 当前本人queued/running的user任务，刷新恢复；不得暴露别人 |

试穿生成要求 `product_type` 或 `prompt` 至少一个非空。AI识别结果不作为强制前置条件。字符串长度有界（prompt/hint 4000字以内）。所有写操作Audit。

任务 `assets.model_generate` / `assets.tryon_analyze` / `assets.tryon` 使用既有user目标，和处理台串行；冲突409。单次出一张。结果：
- 生成模特候选：`{items:[{result_id}],model_id}`。
- 识别：`{product_type,body_part,framing,description,suggested_prompt}`，对枚举/长度验证，不从AI响应读取URL/文件/ID。
- 试穿：`{items:[{result_id}]}`。
失败遵循jobs failed/message，前端允许重试。任务中记录的是发起人的额度。

## 4. workbench 复用

扩展列表 `operation=tryon|model_candidate|process`，按 `op.type` 过滤（旧无type算process），在200条上限之前过滤。

候选 `kind=result/source=ai_generated/op.type=model_candidate`：`model_id,shop_id,body_part,prompt,aspect_ratio,model_name,base_reference_id,confirmed_reference_id?`。不能用通用save保存到商品（400“请确认保存到模特库”）。只走confirm。op里有模板名、模型名和最终提示词。

试穿 `kind=result/source=ai_generated/op.type=tryon`：`shop_id,model_id,model_name,reference_id,body_part,framing,product_type,prompt,aspect_ratio,inputs,final_prompt,model`。主素材引用写asset_id或主上传写input_id。商品素材保存逻辑沿用workbenchSave幂等/parent/CanUpload，不自动转移拍摄编号。读取关联source商品及shop重校；私人结果不能当workbench input复用以绕过权限。原普通处理台列表不受过滤参数影响，前端主动加operation=process避免候选混入。

## 5. AI与提示词

保持现有SubAPI，不换模型默认值、不新增依赖。最小扩展core纯文 `GenerateImage` → `/images/generations` JSON（model/prompt/size/quality），带size的图片编辑方法；保留原EditImage签名/行为。共用额度/重试/超时/PNG验证/错误脱敏；不接受模型给的远程图URL。登记feature `model_create/tryon_analyze/tryon`。

Claude提供提示词文件，后端严格读文件缺失报中文错误：

| 文件 | 替换占位符 |
|---|---|
| 模特-创建.md | `{{description}} {{body_part}} {{extra}}` |
| 模特-局部参考.md | `{{description}} {{body_part}} {{extra}}` |
| 试穿-识别.md | `{{hint}}`；图片长边768用data URL送AIJSON |
| 试穿-生成.md | `{{model_description}} {{body_part}} {{framing}} {{product_type}} {{extra}} {{reference_roles}} {{product_info}}` |

参考角色按主商品、辅商品（如有）、模特顺序写进reference_roles。商品info只用非成本/供应商的必要字段（style/尺寸/材质等，避免暴露敏感字段），上传没有商品info时提示不得猜测精确尺寸。AI模型返回与用户额外要求不执行为代码。

## 6. 验收

离线httptest假AI覆盖：跨店铺/shares/撤权（含workbench保存下载）、无效或不同模特参考、同商品辅图校验、纯文本创建/局部参考多图编辑/size、AI识别异常与手动生成、候选确认幂等和长期文件、归档/队列中改权限、配额不足不调用、失败不发布/回收孤儿文件、结果来源/保存。全量go build/vet/test和前端构建；浏览器隔离库走建模→参考→选图→识别/手填→特写试戴→保存/下载，并测1000px。

真实供应商与人物一致性/商品还原需单独小批验收，未经真实调用不能宣称效果验证通过。
