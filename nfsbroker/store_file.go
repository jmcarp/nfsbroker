package nfsbroker

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"reflect"

	"code.cloudfoundry.org/goshims/ioutilshim"
	"code.cloudfoundry.org/lager"
	"github.com/pivotal-cf/brokerapi"
	"golang.org/x/crypto/bcrypt"
)

const hashKey = "paramsHash"

type fileStore struct {
	fileName     string
	ioutil       ioutilshim.Ioutil
	dynamicState *DynamicState
}

type DynamicState struct {
	InstanceMap map[string]ServiceInstance
	BindingMap  map[string]brokerapi.BindDetails
}

func NewFileStore(
	fileName string,
	ioutil ioutilshim.Ioutil,
) Store {
	return &fileStore{
		fileName: fileName,
		ioutil:   ioutil,
		dynamicState: &DynamicState{
			InstanceMap: make(map[string]ServiceInstance),
			BindingMap:  make(map[string]brokerapi.BindDetails),
		},
	}
}

func (s *fileStore) Restore(logger lager.Logger) error {
	logger = logger.Session("restore-state")
	logger.Info("start")
	defer logger.Info("end")

	serviceData, err := s.ioutil.ReadFile(s.fileName)
	if err != nil {
		logger.Error(fmt.Sprintf("failed-to-read-state-file: %s", s.fileName), err)
		return err
	}

	err = json.Unmarshal(serviceData, s.dynamicState)
	if err != nil {
		logger.Error(fmt.Sprintf("failed-to-unmarshall-state from state-file: %s", s.fileName), err)
		return err
	}
	logger.Info("state-restored", lager.Data{"state-file": s.fileName})

	return err
}

func (s *fileStore) Save(logger lager.Logger) error {
	logger = logger.Session("serialize-state")
	logger.Info("start")
	defer logger.Info("end")

	stateData, err := json.Marshal(s.dynamicState)
	if err != nil {
		logger.Error("failed-to-marshall-state", err)
		return err
	}

	err = s.ioutil.WriteFile(s.fileName, stateData, os.ModePerm)
	if err != nil {
		logger.Error(fmt.Sprintf("failed-to-write-state-file: %s", s.fileName), err)
		return err
	}

	logger.Info("state-saved", lager.Data{"state-file": s.fileName})
	return nil
}

func (s *fileStore) Cleanup() error {
	return nil
}

func (s *fileStore) RetrieveInstanceDetails(id string) (ServiceInstance, error) {
	requestedServiceInstance, found := s.dynamicState.InstanceMap[id]
	if !found {
		return ServiceInstance{}, errors.New(id + " Not Found.")
	}
	return requestedServiceInstance, nil
}

func (s *fileStore) RetrieveBindingDetails(id string) (brokerapi.BindDetails, error) {
	requestedBindingInstance, found := s.dynamicState.BindingMap[id]
	if !found {
		return brokerapi.BindDetails{}, errors.New(id + " Not Found.")
	}
	return requestedBindingInstance, nil
}
func (s *fileStore) CreateInstanceDetails(id string, details ServiceInstance) error {
	s.dynamicState.InstanceMap[id] = details
	return nil
}
func (s *fileStore) CreateBindingDetails(id string, details brokerapi.BindDetails) error {
	storeDetails, err := redactBindingDetails(details)
	if err != nil {
		return err
	}
	s.dynamicState.BindingMap[id] = storeDetails
	return nil
}
func (s *fileStore) DeleteInstanceDetails(id string) error {
	_, found := s.dynamicState.InstanceMap[id]
	if !found {
		return errors.New(id + " Not Found.")
	}

	delete(s.dynamicState.InstanceMap, id)
	return nil
}
func (s *fileStore) DeleteBindingDetails(id string) error {
	_, found := s.dynamicState.BindingMap[id]
	if !found {
		return errors.New(id + " Not Found.")
	}

	delete(s.dynamicState.BindingMap, id)
	return nil
}

func (s *fileStore) IsInstanceConflict(id string, details ServiceInstance) bool {
	if existing, err := s.RetrieveInstanceDetails(id); err == nil {
		if !reflect.DeepEqual(details, existing) {
			return true
		}
	}
	return false
}

func (s *fileStore) IsBindingConflict(id string, details brokerapi.BindDetails) bool {
	if existing, err := s.RetrieveBindingDetails(id); err == nil {
		if existing.AppGUID != details.AppGUID {return true}
		if existing.PlanID != details.PlanID {return true}
		if existing.ServiceID != details.ServiceID {return true}
		if !reflect.DeepEqual(details.BindResource, existing.BindResource) {
			return true
		}
		if (details.Parameters == nil) && (existing.Parameters == nil) { return false }
		if (details.Parameters == nil) || (existing.Parameters == nil) { return true }

		s, err := json.Marshal(details.Parameters)
		if err != nil {
			return true
		}
		h, _ := existing.Parameters[hashKey]
		if bcrypt.CompareHashAndPassword([]byte(h.(string)), s) != nil {return true}
	}
	return false
}

func redactBindingDetails(details brokerapi.BindDetails) (brokerapi.BindDetails, error) {
	if details.Parameters == nil {
		return details, nil
	}
	if len(details.Parameters) == 1 {
		if _, ok := details.Parameters[hashKey]; ok {
			return details, nil
		}
	}

	s, err := json.Marshal(details.Parameters)
	if err != nil {
		return brokerapi.BindDetails{}, err
	}
	s, err = bcrypt.GenerateFromPassword(s, bcrypt.DefaultCost)
	if err != nil {
		return brokerapi.BindDetails{}, err
	}
	details.Parameters = map[string]interface{}{hashKey: string(s)}
	return details, nil
}
