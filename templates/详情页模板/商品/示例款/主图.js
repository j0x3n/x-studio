// 示例款的主图内容（给 运营/主图模板/模板.html 用）。款名、尺寸都是示例，定款后改。
// 照片在同目录 照片/ 里，文件名用拍摄编号。每张图的版式：
//   photo    一张照片铺满，可加左上标签 label、底部文字条 text（一行，数组会用金线隔开）
//   ruler    尺寸标尺图，右上大字 size
//   sizes    同一只耳朵分别戴各个尺寸，items 每格一张、t 写尺寸
//   variants 白底小图并排，items 每个写名字 t
// 照片位可写 pos（3:4 取景位置，如 '50% 35%'）和 pos11（1:1 的取景位置）。
// 规矩（见 运营/淘宝上架操作手册.md 第 4 节）：首图不堆字；首图、上耳、尺寸用纯实拍，不放 AI 背景。
window.MAIN = {
  name: '初雪',
  imgs: [
    { n: 1, layout: 'photo', f: 'D4.jpg', need: '上耳半脸：侧头或撩头发', pos: '50% 45%' },
    { n: 2, layout: 'photo', f: 'E5.jpg', need: '手托托盘，同款多对一起拍', label: '原相机实拍', text: ['同一批珍珠', '每颗光泽略有不同'] },
    { n: 3, layout: 'photo', f: 'B1.jpg', need: '生长纹微距', label: '微距实拍', text: '看得到生长纹 · 真淡水珍珠' },
    { n: 4, layout: 'ruler', f: 'C1.jpg', need: '耳钉放在钢尺上，俯拍', size: '7–8 mm', sizeNote: '珍珠直径', text: '尺子实拍 · 下单前对照大小' },
    { n: 5, layout: 'variants', items: [
        { f: 'A3_白_白底.jpg', need: '白色款白底图', t: '白色' },
        { f: 'A3_粉_白底.jpg', need: '粉色款白底图', t: '粉色' },
        { f: 'A3_紫_白底.jpg', need: '紫色款白底图', t: '紫色' }
      ], text: ['3 色可选', '925 银针 · 附礼盒'] }
  ]
};
