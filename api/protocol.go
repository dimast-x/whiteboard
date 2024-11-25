package api

const (
	KindConnected = iota + 1
	KindUserJoined
	KindUserLeft
	KindStroke
	KindClear
	KindShape
	KindText
)

type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type User struct {
	ID    int    `json:"id"`
	Color string `json:"color"`
}

type Connected struct {
	Kind    int      `json:"kind"`
	Color   string   `json:"color"`
	Users   []User   `json:"users"`
	Strokes []Stroke `json:"strokes"`
	Shapes  []Shape  `json:"shapes"`
	Texts   []Text   `json:"texts"`
}

func NewConnected(color string, users []User, strokes []Stroke, shapes []Shape, texts []Text) *Connected {
	return &Connected{
		Kind:    KindConnected,
		Color:   color,
		Users:   users,
		Strokes: strokes,
		Shapes:  shapes,
		Texts:   texts,
	}
}

type UserJoined struct {
	Kind int  `json:"kind"`
	User User `json:"user"`
}

func NewUserJoined(userID int, color string) *UserJoined {
	return &UserJoined{
		Kind: KindUserJoined,
		User: User{ID: userID, Color: color},
	}
}

type UserLeft struct {
	Kind   int `json:"kind"`
	UserID int `json:"userId"`
}

func NewUserLeft(userID int) *UserLeft {
	return &UserLeft{
		Kind:   KindUserLeft,
		UserID: userID,
	}
}

type Stroke struct {
	Kind   int     `json:"kind"`
	UserID int     `json:"userId"`
	Points []Point `json:"points"`
	Finish bool    `json:"finish"`
}

type Shape struct {
	Kind      int     `json:"kind"`
	UserID    int     `json:"userId"`
	ShapeType string  `json:"shapeType"`
	Start     Point   `json:"start"`
	End       Point   `json:"end"`
	Radius    float64 `json:"radius,omitempty"`
	Color     string  `json:"color"`
}

func NewShape(userID int, shapeType string, start, end Point, radius float64, color string) *Shape {
	return &Shape{
		Kind:      KindShape,
		UserID:    userID,
		ShapeType: shapeType,
		Start:     start,
		End:       end,
		Radius:    radius,
		Color:     color,
	}
}

type Text struct {
	Kind     int    `json:"kind"`
	UserID   int    `json:"userId"`
	Content  string `json:"content"`
	Position Point  `json:"position"`
	FontSize int    `json:"fontSize"`
	Color    string `json:"color"`
}

func NewText(userID int, content string, position Point, fontSize int, color string) *Text {
	return &Text{
		Kind:     KindText,
		UserID:   userID,
		Content:  content,
		Position: position,
		FontSize: fontSize,
		Color:    color,
	}
}

type Clear struct {
	Kind   int `json:"kind"`
	UserID int `json:"userId"`
}
