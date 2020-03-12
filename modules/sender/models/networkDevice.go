package models

import (
	"database/sql"

	"log"
)

// NetworkDevice .
type NetworkDevice struct {
	ID               int    `json:"id"`
	Name             string `json:"name"`
	NetDeviceAssetid string `json:"net_device_assetid"`
	Idc              string `json:"idc"`
	StandardModel    string `json:"standard_model"`
	Status           string `json:"status"`
	Sn               string `json:"sn"`
	DeviceType       string `json:"device_type"`
	Supplier         string `json:"supplier"`
	DeviceModel      string `json:"device_model"`
	DeviceManager    string `json:"device_manager"`
	NetdevFunc       string `json:"netdev_func"`
	ManageIP         string `json:"manage_ip"`
	Shelf            string `json:"shelf"`
	NetworkArea      string `json:"network_area"`
	SchemaVersion    string `json:"schema_version"`
	TorName          string `json:"tor_name"`
}

//GetNameByManageIP 根据manage_ip获取item
func (item *NetworkDevice) GetNameByManageIP(conn *sql.DB) error {

	if item == nil {
		return nil
	}
	var name string
	err := conn.QueryRow("select name from network_device where manage_ip = ?", item.ManageIP).Scan(&name)
	if err != nil {
		log.Println("GetNameByManageIP", err)
		return err
	}
	item.Name = name
	return nil
}
