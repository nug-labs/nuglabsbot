package handlebroadcast

type QuizBroadcaster interface {
	SendQuiz(userID int64, question string, options []string, correctIndex int) error
}
