# internal/objectstore: Revision 对象存储客户端

[中文](README.md) | [English](README.en.md)

`internal/objectstore` 是 cloud 自己的 S3 兼容对象存储客户端,只服务 Revision 这一条链路:为一次交付
尝试签发**单对象键、单方法、限时**的上传授权(Cloud Revision D2/D3),以及在注册 Revision 之前用 HEAD
校验已上传对象的**存在性、大小与 SHA-256**(D4 step 2)。它不决定任何业务语义——授权给谁、何时给、校验
失败后怎么处置都由 `internal/core` 持有;本包只把"签名"与"核验"这两件事做对。

签名是自带实现的 AWS Signature Version 4(SigV4),不引入新依赖:交付链路只需要两个操作(预签一个 PUT、
签一个 HEAD),仓库约定优先标准库而非新增模块。

## 文件

- `objectstore.go`：`New`/`Config` 的配置校验(端点、region、bucket、路径风格、凭据文件)、
  `PresignPut`(返回 `core.UploadGrant`)、`Verify`(HEAD + 大小 + `x-amz-checksum-sha256` 比对)、
  对象键与 bucket 名的注入边界、凭据文件读取(大小上限、去首尾空白)。
- `sigv4.go`：SigV4 的规范化请求构造与签名:预签 PUT 只签 `host`(payload 为 `UNSIGNED-PAYLOAD`)、
  HEAD 签 `host;x-amz-checksum-mode;x-amz-content-sha256;x-amz-date`(payload 为空串摘要);
  逐段 URI 编码(保留 `/`)、查询参数按名排序、摘要的十六进制/大小写归一。
- `objectstore_test.go`：离线单测,见"测试"。

## 依赖与调用方

- 依赖:标准库(`net/http`、`net/url`、`crypto/hmac`、`crypto/sha256`、`encoding/base64`、
  `encoding/hex`),以及 `internal/core`——**仅** `core.UploadGrant` 这一个值类型。
- 调用方:`cmd/server`(在 `object_store` 段已配置时构造客户端并装入
  `store.RevisionObjects`/`store.RevisionUploadTTL`);`internal/core` 通过自己声明的
  `RevisionObjects` 接口(`PresignPut`/`Verify`)按需调用。
- 方向:边是单向的 `internal/objectstore → internal/core`,`internal/core` **不**导入本包——接口按
  "在消费方定义"的约定声明在 `internal/core`,本包只实现它,因此不存在循环。
- 凭据:只以**文件路径**进入配置(`access_key_id_file`/`secret_access_key_file`),值读完即只存在于本
  进程内;不落库、不打日志、不出现在授权里。`credentials` 结构体没有 `String` 方法,本包不格式化它。

## 不变量

- **授权即能力**:一个授权只覆盖一个对象键、一个方法、一段时限;URL 里没有的东西,Node 做不到。
  签名只覆盖 `host`,因此授权不绑定任何请求头——上传方自己计算的 `x-amz-checksum-sha256` 由存储侧校验,
  并由 `Verify` 独立复核。
- **不信任线上声明**:`Verify` 的 `size`/`sha256` 来自 Node 上报,先做形状校验再发请求;大小、摘要任一不等
  即失败,且失败原因不区分——对象缺失、摘要不符、端点不可达对交付是同一件事(D1:交付失败,D5 重试)。
- **无 checksum 即失败**:HEAD 带 `x-amz-checksum-mode: ENABLED`;存储没有返回摘要(它从未收到过
  checksum,因而无法为内容背书)时,校验失败而不是"对象存在即通过"。
- **启动期校验与可达性分离**:端点格式、bucket 名、凭据文件在 `New` 阶段失败即拒绝启动;端点不可达、
  bucket 尚未创建**不是**启动错误(D1),它表现为一次校验失败,由 D5 的重试与放弃窗口兜底。
- **授权时限有界**:非正 TTL 直接拒绝;超过 S3 自身上限(7 天)的 TTL 被夹到上限再签,避免存储接受签名
  后在请求时以 403 拒绝,把配置错误伪装成现象不明的上传失败。
- **凭据不泄漏**:`PresignPut` 只返回 URL 与对象键,不含任何凭据;测试断言 URL 与 `Authorization` 头里
  不出现 secret。

## 已知限制

- **预签 PUT 无法签署校验和头**。D2 的授权在 Node 计算摘要**之前**签发,而真实的 AWS S3 要求所有
  `x-amz-*` 头参与签名,因此该 URL 对 AWS S3 可能被拒;本地 MinIO 对此宽松,本实现按已批准文本实现并在
  `sigv4.go` 就地说明。修复需要改变授权时序或改用 POST policy,属于 ADR 变更。
- **未与真实 S3/MinIO 联调**。Slice 期间 MinIO 镜像拉取失败(`docker.io` 连接被重置),因此本包只有
  `httptest` 双端的确定性测试:它证明请求的形状、绑定与失败判定,不证明真实存储接受这些签名。"本地
  MinIO 集成测试"这条 ADR 要求仍待补齐,证据状态如实登记为 Partial/Missing,不以单测冒充。

## 测试

- 单测全部离线且确定性:固定时钟 + 真实临时凭据文件 + `httptest` S3 双端。
  覆盖授权形状(路径/方法/时限/查询参数/签名形态)、签名对对象键的绑定与可重复性、TTL 两端行为、
  对象键的注入边界、`public_endpoint` 只用于授权而 HEAD 走 `endpoint`、`Verify` 的十种判定分支
  (含无 checksum、非 base64、长度不符)、不可达端点、`New` 的十四种配置错误、凭据文件读取的裁剪规则。
- 与 PostgreSQL、交付状态机的联动由 `integration/agent_run_delivery_test.go` 覆盖。
