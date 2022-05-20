package dice

import (
	"encoding/json"
	"fmt"
	"github.com/fy0/lockfree"
	"github.com/robfig/cron/v3"
	"strings"
	"time"
)

type BanRankType int

const (
	BanRankBanned  BanRankType = -30
	BanRankWarn    BanRankType = -10
	BanRankNormal  BanRankType = 0
	BanRankTrusted BanRankType = 30
)

type BanListInfoItem struct {
	ID      string      `json:"ID"`
	Name    string      `json:"name"`
	Score   int64       `json:"score"`
	Rank    BanRankType `json:"rank"`    // 0 没事 -10警告 -30禁止 30信任
	Times   []int64     `json:"times"`   // 事发时间
	Reasons []string    `json:"reasons"` // 拉黑原因
	Places  []string    `json:"places"`  // 发生地点
	BanTime int64       `json:"banTime"` // 上黑名单时间
}

var BanRankText = map[BanRankType]string{
	BanRankTrusted: "信任",
	BanRankBanned:  "禁止",
	BanRankWarn:    "警告",
	BanRankNormal:  "常规",
}

func (i *BanListInfoItem) toText(d *Dice) string {
	prefix := BanRankText[i.Rank]
	if i.Rank == -10 || i.Rank == -30 {
		return fmt.Sprintf("[%s] <%s>(%s) 原因: %s", prefix, i.Name, i.ID, strings.Join(i.Reasons, ","))
	}
	if i.Rank == 30 {
		return fmt.Sprintf("[%s] <%s> <%s> 原因: %s", prefix, i.Name, i.ID, strings.Join(i.Reasons, ","))
	}
	return ""
}

type BanListInfo struct {
	Parent                   *Dice            `yaml:"-" json:"-"`
	Map                      lockfree.HashMap `yaml:"-" json:"-"`
	BanBehaviorRefuseReply   bool             `yaml:"banBehaviorRefuseReply" json:"banBehaviorRefuseReply"`     //拉黑行为: 拒绝回复
	BanBehaviorRefuseInvite  bool             `yaml:"banBehaviorRefuseInvite" json:"banBehaviorRefuseInvite"`   // 拉黑行为: 拒绝邀请
	BanBehaviorQuitLastPlace bool             `yaml:"banBehaviorQuitLastPlace" json:"banBehaviorQuitLastPlace"` // 拉黑行为: 退出事发群
	ThresholdWarn            int64            `yaml:"thresholdWarn" json:"thresholdWarn"`                       // 警告阈值
	ThresholdBan             int64            `yaml:"thresholdBan" json:"thresholdBan"`                         // 错误阈值
	AutoBanMinutes           int64            `yaml:"autoBanMinutes" json:"autoBanMinutes"`                     // 自动禁止时长

	ScoreReducePerMinute int64 `yaml:"scoreReducePerMinute" json:"scoreReducePerMinute"` // 每分钟下降
	ScoreGroupMuted      int64 `yaml:"scoreGroupMuted" json:"scoreGroupMuted"`           // 群组禁言
	ScoreGroupKicked     int64 `yaml:"scoreGroupKicked" json:"scoreGroupKicked"`         // 群组踢出
	ScoreTooManyCommand  int64 `yaml:"scoreTooManyCommand" json:"scoreTooManyCommand"`   // 刷指令

	JointScorePercentOfGroup   float64 `yaml:"jointScorePercentOfGroup" json:"jointScorePercentOfGroup"`     // 群组连带责任
	JointScorePercentOfInviter float64 `yaml:"jointScorePercentOfInviter" json:"jointScorePercentOfInviter"` // 邀请人连带责任

	cronId cron.EntryID
}

func (i *BanListInfo) Init() {
	// 此为配置装载前的默认设置
	i.BanBehaviorRefuseReply = true
	i.BanBehaviorRefuseInvite = true
	i.BanBehaviorQuitLastPlace = false
	i.ThresholdWarn = 100
	i.ThresholdBan = 200
	i.AutoBanMinutes = 60 * 12 // 12小时

	i.ScoreReducePerMinute = 1
	i.ScoreGroupMuted = 100
	i.ScoreGroupKicked = 200
	i.ScoreTooManyCommand = 100

	i.JointScorePercentOfGroup = 0.5
	i.JointScorePercentOfInviter = 0.3
	i.Map = lockfree.NewHashMap()
}

func (i *BanListInfo) AfterLoads() {
	// 加载完成了
	d := i.Parent
	i.cronId, _ = d.Parent.Cron.AddFunc("@every 1m", func() {
		toDelete := []interface{}{}
		_ = i.Map.Iterate(func(_k interface{}, _v interface{}) error {
			v, ok := _v.(*BanListInfoItem)
			if ok {
				if v.Rank == BanRankNormal || v.Rank == BanRankWarn {
					v.Score -= i.ScoreReducePerMinute
					if v.Score <= 0 {
						// 小于0之后就移除掉
						toDelete = append(toDelete, _k)
					}
				}
			}
			return nil
		})
		for _, j := range toDelete {
			i.Map.Del(j)
		}
	})
}

func (i *BanListInfo) AddScoreBase(uid string, score int64, place string, reason string, ctx *MsgContext) *BanListInfoItem {
	var v *BanListInfoItem
	_v, exists := i.Map.Get(uid)
	if exists {
		v, _ = _v.(*BanListInfoItem)
	}
	if v == nil {
		v = &BanListInfoItem{
			ID:      uid,
			Reasons: []string{},
			Places:  []string{},
		}
	}

	v.Score += score
	v.Name = i.Parent.Parent.TryGetUserName(uid)
	if strings.Contains(uid, "-Group:") {
		v.Name = i.Parent.Parent.TryGetGroupName(uid)
	}
	v.Places = append(v.Places, place)
	v.Reasons = append(v.Reasons, reason)
	v.Times = append(v.Times, time.Now().Unix())

	switch v.Rank {
	case BanRankTrusted:
	// 信任用户，啥都不做
	case BanRankNormal, BanRankWarn:
		if v.Score < i.ThresholdWarn {
			v.Rank = BanRankNormal
		}

		if v.Score >= i.ThresholdWarn {
			v.Rank = BanRankWarn
		}
		if v.Score >= i.ThresholdBan {
			v.Rank = BanRankBanned
			v.BanTime = time.Now().Unix()
		}
	}

	i.Map.Set(uid, v)

	// 发送通知
	if ctx != nil {
		// 警告: XXX 因为等行为，进入警告列表
		// 黑名单: XXX 因为等行为，进入黑名单。将作出以下惩罚：拒绝回复、拒绝邀请、退出事发群
	}

	return v
}

// 返回连带责任人
func (i *BanListInfo) addJointScore(uid string, place string, reason string, ctx *MsgContext) (string, BanRankType) {
	d := i.Parent
	if i.JointScorePercentOfGroup > 0 {
		score := i.JointScorePercentOfGroup * float64(i.ScoreGroupMuted)
		i.AddScoreBase(place, int64(score), place, reason, ctx)
	}
	if i.JointScorePercentOfInviter > 0 {
		group := d.ImSession.ServiceAtNew[place]
		if group != nil && group.InviteUserId != "" {
			rank := i.NoticeCheckPrepare(group.InviteUserId)
			score := i.JointScorePercentOfInviter * float64(i.ScoreGroupMuted)
			i.AddScoreBase(group.InviteUserId, int64(score), place, reason, ctx)

			//text := fmt.Sprintf("提醒: 你邀请的骰子在群组<%s>中被禁言/踢出/指令刷屏了", group.GroupName)
			//ReplyPersonRaw(ctx, &Message{Sender: SenderBase{UserId: group.InviteUserId}}, text, "")
			return group.InviteUserId, rank
		}
	}
	return "", BanRankNormal
}

func (i *BanListInfo) NoticeCheckPrepare(uid string) BanRankType {
	item := i.GetById(uid)
	if item != nil {
		return item.Rank
	}
	return BanRankNormal
}

func (i *BanListInfo) NoticeCheck(uid string, place string, oldRank BanRankType, ctx *MsgContext) BanRankType {
	log := i.Parent.Logger
	item := i.GetById(uid)
	if item != nil {
		curRank := item.Rank
		if oldRank != curRank && (curRank == BanRankWarn || curRank == BanRankBanned) {
			txt := fmt.Sprintf("黑名单等级提升: %v", item.toText(i.Parent))
			log.Info(txt)

			if ctx != nil {
				// 做出通知
				ctx.Notice(txt)

				// 发给当事人
				ReplyPersonRaw(ctx, &Message{Sender: SenderBase{UserId: uid}}, "提醒：你引发了黑名单事件:\n"+txt, "")

				// 发给当事群
				time.Sleep(1 * time.Second)
				ReplyGroupRaw(ctx, &Message{GroupId: place}, "提醒: 当前群组发生黑名单事件\n"+txt, "")

				// 发给邀请者
				time.Sleep(1 * time.Second)
				group := i.Parent.ImSession.ServiceAtNew[place]
				if group != nil && group.InviteUserId != "" {
					text := fmt.Sprintf("提醒: 你邀请的骰子在群组<%s>(%s)中遭遇黑名单事件:\n%v", group.GroupName, group.GroupId, txt)
					ReplyPersonRaw(ctx, &Message{Sender: SenderBase{UserId: group.InviteUserId}}, text, "")
				}
			}

			if curRank == BanRankBanned {
				if i.BanBehaviorQuitLastPlace {
					if ctx != nil {
						ReplyGroupRaw(ctx, &Message{GroupId: place}, "因拉黑惩罚选项中有“退出事发群”，即将自动退群。", "")
						time.Sleep(1 * time.Second)
						ctx.EndPoint.Adapter.QuitGroup(ctx, place)
					}
				}
			}
		}
	}
	return 0
}

// AddScoreByGroupMuted 群组禁言
func (i *BanListInfo) AddScoreByGroupMuted(uid string, place string, ctx *MsgContext) {
	rank := i.NoticeCheckPrepare(uid)

	i.AddScoreBase(uid, i.ScoreGroupMuted, place, "禁言骰子", ctx)
	inviterId, inviterRank := i.addJointScore(uid, place, "连带责任:禁言骰子", ctx)

	i.NoticeCheck(uid, place, rank, ctx)
	if inviterId != "" && inviterId != uid {
		// 如果连带责任人与操作者不是同一人，进行单独计算
		i.NoticeCheck(inviterId, place, inviterRank, ctx)
	}
}

// AddScoreByGroupKicked 群组踢出
func (i *BanListInfo) AddScoreByGroupKicked(uid string, place string, ctx *MsgContext) {
	rank := i.NoticeCheckPrepare(uid)

	i.AddScoreBase(uid, i.ScoreGroupKicked, place, "踢出骰子", ctx)
	inviterId, inviterRank := i.addJointScore(uid, place, "连带责任:踢出骰子", ctx)

	i.NoticeCheck(uid, place, rank, ctx)
	if inviterId != "" && inviterId != uid {
		// 如果连带责任人与操作者不是同一人，进行单独计算
		i.NoticeCheck(inviterId, place, inviterRank, ctx)
	}
}

func (i *BanListInfo) toJSON() []byte {
	data, err := json.Marshal(i)
	if err != nil {
		return nil
	}

	tmp := map[string]interface{}{}
	err = json.Unmarshal(data, &tmp)
	if err != nil {
		return nil
	}

	dict := map[string]*BanListInfoItem{}
	err = i.Map.Iterate(func(_k interface{}, _v interface{}) error {
		k, ok1 := _k.(string)
		v, ok2 := _v.(*BanListInfoItem)
		if ok1 && ok2 {
			dict[k] = v
		}
		return nil
	})
	if err != nil {
		return nil
	}

	tmp["map"] = dict
	marshal, err := json.Marshal(tmp)
	if err != nil {
		return nil
	}
	return marshal
}

func (i *BanListInfo) loadJSON(data []byte) {
	tmp := map[string]interface{}{}
	err := json.Unmarshal(data, &tmp)
	if err != nil {
		return
	}

	// 进行常规转换
	err = json.Unmarshal(data, &i)
	if err != nil {
		return
	}

	// 如果存在map进行转换
	if val, ok := tmp["map"]; ok {
		data2, err := json.Marshal(val)
		if err != nil {
			return
		}
		dict := map[string]*BanListInfoItem{}
		err = json.Unmarshal(data2, &dict)
		if err != nil {
			return
		}
		realDict := lockfree.NewHashMap()
		for k, v := range dict {
			realDict.Set(k, v)
		}
	}
}

func (i *BanListInfo) MapToJSON() []byte {
	dict := map[string]*BanListInfoItem{}
	err := i.Map.Iterate(func(_k interface{}, _v interface{}) error {
		k, ok1 := _k.(string)
		v, ok2 := _v.(*BanListInfoItem)
		if ok1 && ok2 {
			dict[k] = v
		}
		return nil
	})
	if err != nil {
		return nil
	}

	marshal, err := json.Marshal(dict)
	if err != nil {
		return nil
	}
	return marshal
}

func (i *BanListInfo) LoadMapFromJSON(data []byte) {
	// 如果存在map进行转换
	dict := map[string]*BanListInfoItem{}
	err := json.Unmarshal(data, &dict)
	if err != nil {
		return
	}
	realDict := lockfree.NewHashMap()
	for k, v := range dict {
		realDict.Set(k, v)
	}
	i.Map = realDict
}

func (i *BanListInfo) GetById(uid string) *BanListInfoItem {
	var v *BanListInfoItem
	_v, exists := i.Map.Get(uid)
	if exists {
		v, _ = _v.(*BanListInfoItem)
	}
	return v
}

func (i *BanListInfo) SetTrustById(uid string, place string, reason string) {
	v := i.GetById(uid)
	if v == nil {
		v = &BanListInfoItem{
			ID:      uid,
			Reasons: []string{},
			Places:  []string{},
		}
	}
	v.Rank = BanRankTrusted
	v.Name = i.Parent.Parent.TryGetUserName(uid)
	if strings.Contains(uid, "-Group:") {
		v.Name = i.Parent.Parent.TryGetGroupName(uid)
	}
	v.Places = append(v.Places, place)
	v.Reasons = append(v.Reasons, reason)
	v.Times = append(v.Times, time.Now().Unix())
	i.Map.Set(uid, v)
}