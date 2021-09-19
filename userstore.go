/*
 * What space users can do. Associating RFID with user capabilities.
 * This is currently just storing things sequentially and storing in
 * the file
 */

package main

import (
	"encoding/csv"
	"log"
	"net/http"
	"os"
	"strconv"
)

type User struct {
	RFID        string `json:"user_rfid"`
	Name        string `json:"user_name"`
	Printer3D   bool   `json:"perm_printer3d"`
	Laser       bool   `json:"perm_laser"`
	Vinyl       bool   `json:"perm_vinyl"`
	CNC         bool   `json:"perm_cnc"`
	Tablesaw    bool   `json:"perm_tablesaw"`
	Electronics bool   `json:"perm_electronics"`
}

func BoolFromColumn(columns []string, index int) bool {
	if len(columns) <= index {
		return false
	}
	value, err := strconv.ParseBool(columns[index])
	return err == nil && value
}

func NewUserFromCSV(reader *csv.Reader) (user *User, done bool) {
	line, err := reader.Read()
	if err != nil {
		return nil, true
	}
	if len(line) < 2 {
		log.Printf("line len is %v\n", len(line))
		return nil, false
	}
	log.Printf("create user\n")
	user = &User{
		RFID: line[0],
		Name: line[1],

		Printer3D:   BoolFromColumn(line, 2),
		Laser:       BoolFromColumn(line, 3),
		Vinyl:       BoolFromColumn(line, 4),
		CNC:         BoolFromColumn(line, 5),
		Tablesaw:    BoolFromColumn(line, 6),
		Electronics: BoolFromColumn(line, 7),
	}
	return user, false
}

func (user *User) WriteCSV(writer *csv.Writer) {
	var fields []string = make([]string, 8)
	fields[0] = user.RFID
	fields[1] = user.Name
	fields[2] = strconv.FormatBool(user.Printer3D)
	fields[3] = strconv.FormatBool(user.Laser)
	fields[4] = strconv.FormatBool(user.Vinyl)
	fields[5] = strconv.FormatBool(user.CNC)
	fields[6] = strconv.FormatBool(user.Tablesaw)
	fields[7] = strconv.FormatBool(user.Electronics)
	writer.Write(fields)
}

func BoolFromForm(r *http.Request, name string) bool {
	return len(r.FormValue(name)) > 0
}

func (user *User) UpdateFromFormValues(r *http.Request) {
	// Don't update RFID code
	user.Name = r.FormValue("user_name")
	user.Printer3D = BoolFromForm(r, "perm_printer3d")
	user.Laser = BoolFromForm(r, "perm_laser")
	user.Vinyl = BoolFromForm(r, "perm_vinyl")
	user.CNC = BoolFromForm(r, "perm_cnc")
	user.Tablesaw = BoolFromForm(r, "perm_tablesaw")
	user.Electronics = BoolFromForm(r, "perm_electronics")
}

type UserStore struct {
	filename  string
	userList  []*User          // Sequence of users as in file
	code2user map[string]*User // RFID to user lookup
}

func NewUserStore(storeFilename string) *UserStore {
	s := &UserStore{
		filename:  storeFilename,
		userList:  make([]*User, 0, 10),
		code2user: make(map[string]*User),
	}
	if !s.readDatabase() {
		return nil
	}
	return s
}

func (s *UserStore) get_user(code string) *User {
	return s.code2user[code]
}

func (s *UserStore) createEmptyUser(code string) *User {
	return &User{
		RFID: code,
	}
}

// Function that updates a particular user. It will be called by
// the UserStore with a pointer to the user. Tha Callee returns
// 'true' if the transaction is to be performed
type ModifyFun func(user *User) bool

func (s *UserStore) InsertOrUpdateUser(rfid string, modifier ModifyFun) {
	// Create a copy first
	existing_user := s.get_user(rfid)
	var to_modify User
	if existing_user != nil {
		to_modify = *existing_user
	} else {
		to_modify = *s.createEmptyUser(rfid)
	}
	if modifier(&to_modify) {
		if to_modify.RFID != rfid || to_modify.Name == "" {
			log.Printf("Insufficient data to modify\n")
			return
		}
		if existing_user != nil {
			// Update the existing pointer
			*existing_user = to_modify
		} else {
			// store new.
			s.userList = append(s.userList, &to_modify)
			s.code2user[to_modify.RFID] = &to_modify
		}
		s.writeDatabase()
	}
}

func (s *UserStore) readDatabase() bool {
	if s.filename == "" {
		log.Println("RFID-user file not provided")
		return false
	}
	f, err := os.Open(s.filename)
	if err != nil {
		log.Println("Could not read RFID user-file", err)
		return false
	}

	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1 //variable length fields

	total := 0
	log.Printf("Reading %s", s.filename)
	for {
		user, done := NewUserFromCSV(reader)
		if done {
			break
		}
		if user == nil {
			continue // e.g. due to comment or short line
		}
		s.userList = append(s.userList, user)
		s.code2user[user.RFID] = user
		total++
	}
	log.Printf("Read %d users from %s", total, s.filename)
	return true
}

// Write content of the 'user database' to temp CSV file.
func (s *UserStore) writeTempCSV(filename string) (bool, string) {
	os.Remove(filename)
	f, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return false, err.Error()
	}
	defer f.Close()
	writer := csv.NewWriter(f)
	for _, user := range s.userList {
		if user != nil {
			user.WriteCSV(writer)
		}
	}
	writer.Flush()
	if writer.Error() != nil {
		log.Println(writer.Error())
		return false, writer.Error().Error()
	}
	return true, ""
}

func (s *UserStore) writeDatabase() (bool, string) {
	// First, dump out the database to a temporary file and
	// make sure it succeeds.
	tmpFilename := s.filename + ".tmp"
	if ok, msg := s.writeTempCSV(tmpFilename); !ok {
		return false, msg
	}

	// Alright, good. Atomic rename.
	os.Rename(tmpFilename, s.filename)

	return true, ""
}
