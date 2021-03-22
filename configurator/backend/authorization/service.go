package authorization

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jitsucom/jitsu/configurator/storages"
	uuid "github.com/satori/go.uuid"
	"github.com/spf13/viper"
	"golang.org/x/crypto/bcrypt"
)

const (
	UsersInfoCollection = "users_info"
	RedisType           = "redis"
	FirebaseType        = "firebase"
)

var ErrUserNotFound = errors.New("User wasn't found")
var ErrUserExists = errors.New("User already exists")
var ErrOldPasswordIncorrect = errors.New("Old password is incorrect")
var ErrResetIdNotFound = errors.New("Reset id wasn't found")
var ErrIncorrectPassword = errors.New("Incorrect password")
var ErrExpiredToken = errors.New("Expired token")
var ErrUnknownToken = errors.New("Unknown token")

type Service struct {
	authProvider Provider

	configurationsStorage storages.ConfigurationsStorage
}

func NewService(ctx context.Context, authViper *viper.Viper, storage storages.ConfigurationsStorage) (*Service, error) {
	var authProvider Provider
	var err error
	if authViper.IsSet("firebase") {
		firebaseViper := authViper.Sub("firebase")

		authProvider, err = NewFirebaseProvider(ctx, firebaseViper.GetString("project_id"), firebaseViper.GetString("credentials_file"), authViper.GetString("admin_domain"))
		if err != nil {
			return nil, err
		}
	} else if authViper.IsSet("redis") {
		redisViper := authViper.Sub("redis")
		host := redisViper.GetString("host")
		if host == "" {
			return nil, errors.New("auth.redis.host is required")
		}

		port := redisViper.GetInt("port")
		if port == 0 {
			return nil, errors.New("auth.redis.port is required")
		}

		redisPassword := redisViper.GetString("password")

		accessSecret := redisViper.GetString("access_secret")
		if accessSecret == "" {
			return nil, errors.New("auth.redis.access_secret is required")
		}

		refreshSecret := redisViper.GetString("refresh_secret")
		if refreshSecret == "" {
			return nil, errors.New("auth.redis.refresh_secret is required")
		}

		authProvider, err = NewRedisProvider(host, redisPassword, accessSecret, refreshSecret, port)
		if err != nil {
			return nil, err
		}
	} else {
		return nil, errors.New("Unknown authorization type. Supported: firebase, redis")
	}

	return &Service{authProvider: authProvider, configurationsStorage: storage}, nil
}

//Authenticate verify acess token and return user id
func (s *Service) Authenticate(token string) (string, error) {
	userId, err := s.authProvider.VerifyAccessToken(token)
	if err != nil {
		return "", err
	}

	return userId, nil
}

//GetProjectId return projectId from storage by userId
func (s *Service) GetProjectId(userId string) (string, error) {
	usersInfoResponse, err := s.configurationsStorage.Get(UsersInfoCollection, userId)
	if err != nil {
		return "", err
	}

	var userInfo UserInfo
	err = json.Unmarshal(usersInfoResponse, &userInfo)
	if err != nil {
		return "", err
	}

	if userInfo.Project.Id == "" {
		return "", fmt.Errorf("_project._id is not set for user %s", userId)
	}

	return userInfo.Project.Id, nil
}

//GenerateUserToken generate access token for userId
func (s *Service) GenerateUserToken(userId string) (string, error) {
	return s.authProvider.GenerateUserAccessToken(userId)
}

//IsAdmin return true if the user id admin
func (s *Service) IsAdmin(userId string) (bool, error) {
	return s.authProvider.IsAdmin(userId)
}

//SignUp check existence of the email and create a new User
//return TokenDetails with JWT access token and refresh token
func (s *Service) SignUp(email, password string) (*TokenDetails, error) {
	_, err := s.authProvider.GetUserByEmail(email)
	if err == nil {
		return nil, ErrUserExists
	}

	if err != ErrUserNotFound {
		return nil, err
	}

	hashedPassword, err := s.hashAndSalt(password)
	if err != nil {
		return nil, err
	}

	userId := "user-" + uuid.NewV4().String()
	err = s.authProvider.SaveUser(&User{
		ID:             userId,
		Email:          email,
		HashedPassword: hashedPassword,
	})
	if err != nil {
		return nil, err
	}

	return s.authProvider.CreateTokens(userId)
}

//SignIn check email and password and return TokenDetails with JWT access token and refresh token
func (s *Service) SignIn(email, password string) (*TokenDetails, error) {
	user, err := s.authProvider.GetUserByEmail(email)
	if err != nil {
		return nil, err
	}

	err = s.comparePasswords(user.HashedPassword, password)
	if err != nil {
		return nil, ErrIncorrectPassword
	}

	return s.authProvider.CreateTokens(user.ID)
}

//SignOut delete token from authorization storage
func (s *Service) SignOut(token string) error {
	return s.authProvider.DeleteToken(token)
}

//CreateResetId return rest id and email
func (s *Service) CreateResetId(email string) (string, string, error) {
	user, err := s.authProvider.GetUserByEmail(email)
	if err != nil {
		return "", "", err
	}

	resetId := "reset-" + uuid.NewV4().String()

	err = s.authProvider.SavePasswordResetId(resetId, user.ID)
	if err != nil {
		return "", "", err
	}

	return resetId, user.Email, nil
}

//ChangePassword change user password and delete all tokens
func (s *Service) ChangePassword(resetId, newPassword string) (*TokenDetails, error) {
	user, err := s.authProvider.GetUserByResetId(resetId)
	if err != nil {
		return nil, err
	}

	hashedPassword, err := s.hashAndSalt(newPassword)
	if err != nil {
		return nil, err
	}
	user.HashedPassword = hashedPassword

	err = s.authProvider.DeleteAllTokens(user.ID)
	if err != nil {
		return nil, err
	}

	err = s.authProvider.SaveUser(user)
	if err != nil {
		return nil, err
	}

	err = s.authProvider.DeletePasswordResetId(resetId)
	if err != nil {
		return nil, err
	}

	return s.authProvider.CreateTokens(user.ID)
}

func (s *Service) Refresh(refreshToken string) (*TokenDetails, error) {
	return s.authProvider.RefreshTokens(refreshToken)
}

func (s *Service) UsersExist() (bool, error) {
	return s.authProvider.UsersExist()
}

func (s *Service) GetAuthorizationType() string {
	return s.authProvider.Type()
}

func (s *Service) Close() error {
	if err := s.authProvider.Close(); err != nil {
		return fmt.Errorf("Error closing authorization provider: %v", err)
	}

	return nil
}

func (s *Service) hashAndSalt(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		return "", err
	}

	return string(hash), nil
}

func (s *Service) comparePasswords(hashedPassword string, password string) error {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	if err != nil {
		return err
	}

	return nil
}
